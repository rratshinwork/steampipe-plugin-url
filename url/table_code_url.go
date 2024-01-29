package url

import (
	// "compress/gzip"
	"context"
	"encoding/csv"
	"fmt"
	"io"
	// "os"
	"strings"

	// "github.com/dimchansky/utfbom"
	"github.com/turbot/go-kit/helpers"
	"github.com/turbot/steampipe-plugin-sdk/v5/grpc/proto"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin"
	// "github.com/turbot/steampipe-plugin-sdk/v5/connection"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin/transform"
	// "github.com/hashicorp/go-hclog"
	// "github.com/turbot/steampipe-plugin-sdk/v5/grpc/proto"

	"net/http"
	// "github.com/davecgh/go-spew/spew"
	// "sync"
	// "time"
	// "strconv"
	// "regexp"
	// "unicode"
)


func tableData(ctx context.Context, connection *plugin.Connection) (*plugin.Table) {

	urlConfig := GetConfig(connection)
	var dataURL string
	if urlConfig.DataURL != nil {
		dataURL = *urlConfig.DataURL
	}

	cols := []*plugin.Column{}

	sa_rows, sa_column_map, _ := readData(ctx, dataURL)
	for s_column_name, s_column_type := range sa_column_map {
		if s_column_type == "INTEGER" {
			cols = append(cols, &plugin.Column{Name: s_column_name, Type: proto.ColumnType_INT, Transform: transform.FromField(helpers.EscapePropertyName(s_column_name))})
		} else if s_column_type == "NUMERIC" {
			cols = append(cols, &plugin.Column{Name: s_column_name, Type: proto.ColumnType_DOUBLE, Transform: transform.FromField(helpers.EscapePropertyName(s_column_name))})
		} else if s_column_type == "DATE" || s_column_type == "TIMESTAMP" {
			cols = append(cols, &plugin.Column{Name: s_column_name, Type: proto.ColumnType_TIMESTAMP, Transform: transform.FromField(helpers.EscapePropertyName(s_column_name))})
		} else {
			cols = append(cols, &plugin.Column{Name: s_column_name, Type: proto.ColumnType_STRING, Transform: transform.FromField(helpers.EscapePropertyName(s_column_name))})
		}
	}


	return &plugin.Table {
		Name: "http",
		List: &plugin.ListConfig{
			Hydrate: listDataWithURL(sa_rows),
		},
		Columns: cols,
	}
}


func listDataWithURL (sa_rows []map[string]string) func(ctx context.Context, d *plugin.QueryData, h *plugin.HydrateData) (interface{}, error) {
	return func(ctx context.Context, d *plugin.QueryData, h *plugin.HydrateData) (interface{}, error) {
		for _, sm_row := range sa_rows {
			d.StreamListItem(ctx, sm_row)
		}
		return nil, nil
	}
}


func readData(ctx context.Context, s_url string) ([]map[string]string, map[string]string, error) {

	var sa_data []map[string]string
	// var sa_rows [][] string
	var sa_columns [] string

	resp, err := http.Get(s_url)
    if err != nil {
        plugin.Logger(ctx).Error("readData Error < " + err.Error() + ">")
    }
    defer resp.Body.Close()

    var sb_data strings.Builder
    var i_buff_total int = 0
    var i_buff_max int = 20000000 // 20 MB
    var i_read_buff int = 1000000 // 1 MB
    var b_eof bool = false
    buff := make([]byte, i_read_buff)  
    for i_buff_total < i_buff_max {
        var bytesRead int  
        bytesRead, err = resp.Body.Read(buff)
        if err == io.EOF {
            b_eof = true
            if bytesRead <= 0 {  
                break  
            }
        }
        if err != nil {
        	plugin.Logger(ctx).Error("readData Error < " + err.Error() + " >")
        }
        s_data := string(buff[:bytesRead])
        s_data = strings.Replace(s_data, "\r\n", "\n", -1) // handle DOS/Windows newlines
        s_data = strings.Replace(s_data, "\r", "\n", -1) // handle DOS/Windows newlines
        i_buff_total = len(sb_data.String())
        sb_data.WriteString(sanitizeUTF8(s_data))
    }

    s_final_data := sb_data.String()
    if b_eof == false {
        var i_final_newline int = strings.LastIndex(s_final_data, "\n")
        s_final_data = s_final_data[0 :i_final_newline]
    }

    detector := New()
	sampleLines := 4
	detector.Configure(&sampleLines, nil)
	delimiters := detector.DetectDelimiter(strings.NewReader(s_final_data), '"')

    nr := csv.NewReader(strings.NewReader(s_final_data))
    s_separator := strings.Replace(delimiters[0], "/", "//", -1)
    nr.Comma = GetSeparator(s_separator)

	records, err := nr.ReadAll()
	if err != nil {
		plugin.Logger(ctx).Error(err.Error())
	}

	for _, s_value := range records[0] {
		sa_columns = append(sa_columns, s_value)
	}

	sa_column_map := make(map[string]string)
	for idx, s_column := range sa_columns {
		i_false_date := 0
		i_false_integer := 0
		i_false_numeric := 0
		s_data_type := "STRING"
		for idx0, sa_row := range records {
			if idx0 == 0 {
				continue;
			}
			s_value := sa_row[idx]
			if !isDate(s_value) {
				i_false_date++
			}
			if !isInteger(s_value) {
				i_false_integer++
			}
			if !isNumeric(s_value) {
				i_false_numeric++
			}
			if i_false_date > 0 && i_false_integer > 0 && i_false_numeric > 0 {
				break
			}
		}
		if i_false_date == 0 {
			s_data_type = "DATE"
		} else if i_false_integer == 0 {
			s_data_type = "INTEGER"
		} else if i_false_numeric == 0 {
			s_data_type = "NUMERIC"
		}
		
		sa_column_map[s_column] = s_data_type
	}

	for idx, record := range records {
		if idx == 0 {
			continue
		}
		sm_row := map[string]string{}
		for idx0, s_value := range record {
			sm_row[sa_columns[idx0]] = s_value
		}
		sa_data = append(sa_data, sm_row)
	}

	return sa_data, sa_column_map, nil

}

// A valid header row has no empty values or duplicate values
func validHeader(ctx context.Context, header []string) (bool, string) {
	keys := make(map[string]bool)
	for idx, i := range header {
		// Check for empty column names
		if len(i) == 0 {
			return false, fmt.Sprintf("header row has empty value in field %d", idx)
		}
		// Check for duplicate column names
		_, ok := keys[i]
		if ok {
			return false, fmt.Sprintf("header row has duplicate value in field %d", idx)
		}
		keys[i] = true
	}

	// No empty or duplicate column names found
	return true, ""
}
