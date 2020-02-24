// estab exports elasticsearch fields as tab separated values
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/pprof"
	"strconv"
	"strings"

	"github.com/OwnLocal/goes"
	"github.com/hochgi/estab"
)

func main() {

	host := flag.String("host", "localhost", "elasticsearch host")
	port := flag.String("port", "9200", "elasticsearch port")
	indicesString := flag.String("indices", "", "indices to search (or all)")
	fieldsString := flag.String("f", "_id _index", "field or fields space separated")
	timeout := flag.String("timeout", "10m", "scroll timeout")
	size := flag.Int("size", 10000, "scroll batch size")
	nullValue := flag.String("null", "NOT_AVAILABLE", "value for empty fields")
	separator := flag.String("separator", "|", "separator to use for multiple field values")
	delimiter := flag.String("delimiter", "\t", "column delimiter")
	limit := flag.Int("limit", -1, "maximum number of docs to return (return all by default)")
	version := flag.Bool("v", false, "prints current program version")
	cpuprofile := flag.String("cpuprofile", "", "write cpu profile to file")
	queryString := flag.String("query", "", "custom query to run")
	raw := flag.Bool("raw", false, "stream out the raw json records")
	header := flag.Bool("header", false, "output header row with field names")
	singleValue := flag.Bool("1", false, "one value per line (works only with a single column in -f)")
	zeroAsNull := flag.Bool("zero-as-null", false, "treat zero length strings as null values")
	precision := flag.Int("precision", 0, "precision for numeric output")

	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		pprof.StartCPUProfile(f)
		defer pprof.StopCPUProfile()
	}

	if *version {
		fmt.Println(estab.Version)
		os.Exit(0)
	}

	var query map[string]interface{}
	if *queryString == "" {
		query = map[string]interface{}{
			"query": map[string]interface{}{
				"match_all": map[string]interface{}{},
			},
		}
	} else {
		err := json.Unmarshal([]byte(*queryString), &query)
		if err != nil {
			log.Fatal(err)
		}
	}

	indices := strings.Fields(*indicesString)
	fields := strings.Fields(*fieldsString)

	if *raw && *singleValue {
		log.Fatal("-1 xor -raw ")
	}

	if *singleValue && len(fields) > 1 {
		log.Fatalf("-1 works only with a single column, %d given: %s\n", len(fields), strings.Join(fields, " "))
	}

	if !*raw {
		query["fields"] = fields
	}

	conn := goes.NewClient(*host, *port)
	scanResponse, err := conn.Scan(query, indices, []string{""}, *timeout, *size)
	if err != nil {
		log.Fatal(err)
	}

	w := bufio.NewWriter(os.Stdout)
	defer w.Flush()
	i := 0

	if *header {
		fmt.Fprintln(w, strings.Join(fields, *delimiter))
	}

	for {
		scrollResponse, err := conn.Scroll(scanResponse.ScrollID, *timeout)
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Fatal(err)
		}
		if len(scrollResponse.Hits.Hits) == 0 {
			break
		}
		for _, hit := range scrollResponse.Hits.Hits {
			if i == *limit {
				return
			}
			if *raw {
				b, err := json.Marshal(hit)
				if err != nil {
					log.Fatal(err)
				}
				fmt.Fprintln(w, string(b))
				continue
			}

			var columns []string
			for _, f := range fields {
				var c []string
				switch f {
				case "_id":
					c = append(c, hit.ID)
				case "_index":
					c = append(c, hit.Index)
				case "_type":
					c = append(c, hit.Type)
				case "_score":
					c = append(c, strconv.FormatFloat(hit.Score, 'f', 6, 64))
				default:
					switch value := hit.Fields[f].(type) {
					case nil:
						c = []string{*nullValue}
					case []interface{}:
						for _, e := range value {
							switch e.(type) {
							case string:
								s := e.(string)
								if s == "" && *zeroAsNull {
									c = append(c, *nullValue)
								} else {
									c = append(c, e.(string))
								}
							case float64:
								c = append(c, strconv.FormatFloat(e.(float64), 'f', *precision, 64))
							case bool:
								c = append(c, strconv.FormatBool(e.(bool)))
							}
						}
					default:
						log.Fatalf("unknown field type in response: %+v\n", hit.Fields[f])
					}
				}
				if *singleValue {
					for _, value := range c {
						fmt.Fprintln(w, value)
					}
				} else {
					columns = append(columns, strings.Join(c, *separator))
				}
			}
			if !*singleValue {
				fmt.Fprintln(w, strings.Join(columns, *delimiter))
			}
			i++
		}
	}
}
