package reader

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-kit/kit/endpoint"
	"github.com/louvri/gokrt/sys_key"
	sql "github.com/louvri/gosl"
)

func Middleware(filename string, size int, decoder func(data interface{}) interface{}, useTransaction bool, ignoreError bool) endpoint.Middleware {
	return func(next endpoint.Endpoint) endpoint.Endpoint {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			var reader io.Reader
			if tmp, ok := ctx.Value(sys_key.FILE_KEY).(map[string]interface{}); tmp != nil && ok {
				reader = tmp[filename].(io.Reader)
			} else {
				return nil, nil
			}
			scanner := bufio.NewScanner(reader)
			first := true
			var columns []string
			tobeInserted := make([]map[string]interface{}, 0)
			exec := func(ctx context.Context, data map[string]interface{}, flush bool) (interface{}, error) {
				isEmpty := len(data) == 0
				if useTransaction {
					var responses []interface{}
					if len(tobeInserted) > size || flush {
						responses = make([]interface{}, 0)
						err := sql.RunInTransaction(ctx, func(ctx context.Context) error {
							for _, item := range tobeInserted {
								var err error
								var response interface{}
								if decoder != nil {
									response, err = next(ctx, decoder(item))
								} else {
									response, err = next(ctx, item)
								}
								responses = append(responses, response)
								if err != nil {
									return err
								}
							}
							return nil
						})
						if err != nil {
							return responses, err
						}
						tobeInserted = make([]map[string]interface{}, 0)
					}
					if !isEmpty {
						tobeInserted = append(tobeInserted, data)
					}
					return responses, nil
				} else if !isEmpty {
					var err error
					var response interface{}
					if decoder != nil {
						response, err = next(ctx, decoder(data))
					} else {
						response, err = next(ctx, data)
					}
					if err != nil {
						return response, err
					}
					return response, nil
				}
				return nil, nil
			}
			var err error
			var response interface{}
			lineNumber := 1
			for scanner.Scan() {
				text := scanner.Text()
				text = strings.ReplaceAll(text, "\ufeff", "")
				text = strings.TrimSpace(text)
				if first {
					columns = strings.Split(text, ";")
					first = false
					ctx = context.WithValue(ctx, sys_key.SOF, true)
				} else {
					values := strings.Split(text, ";")
					//check values
					isempty := true
					for _, item := range values {
						isempty = isempty && (item == "" || item == " ")
					}
					if !isempty {
						data := make(map[string]interface{})
						for i, column := range columns {
							data[column] = values[i]
						}
						data["lineNumber"] = lineNumber
						response, err = exec(ctx, data, false)
						if err != nil && !ignoreError {
							return nil, fmt.Errorf("%s:%s", "csv_reader_middleware:", err.Error())
						}
					}
					ctx = context.WithValue(ctx, sys_key.SOF, false)
				}
				lineNumber++
				time.Sleep(0)
			}
			if tmp, err := exec(ctx, nil, true); err != nil && !ignoreError {
				return nil, fmt.Errorf("%s:%s", "csv_reader_middleware:", err.Error())
			} else if tmp != nil {
				response = tmp
			}
			if err := scanner.Err(); err != nil && !ignoreError {
				return nil, fmt.Errorf("%s:%s", "csv_reader_middleware:", err.Error())
			}
			ctx = context.WithValue(ctx, sys_key.EOF, "eof")
			if tmp, err := next(ctx, nil); err != nil {
				return nil, fmt.Errorf("%s:%s", "csv_reader_middleware:", err.Error())
			} else if tmp != nil {
				response = tmp
			}
			return response, nil
		}
	}
}
