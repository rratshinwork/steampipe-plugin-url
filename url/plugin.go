package url

import (
	"context"
	// "errors"
	// "os"
	// "path/filepath"
	// "strings"

	"github.com/turbot/steampipe-plugin-sdk/v5/plugin"
	"github.com/turbot/steampipe-plugin-sdk/v5/plugin/transform"

	// "github.com/davecgh/go-spew/spew"
)

func Plugin(ctx context.Context) *plugin.Plugin {
	p := &plugin.Plugin{
		Name: "steampipe-plugin-url",
		ConnectionConfigSchema: &plugin.ConnectionConfigSchema{
			NewInstance: ConfigInstance,
		},
		DefaultTransform: transform.FromGo().NullIfZero(),
		SchemaMode:       plugin.SchemaModeDynamic,
		TableMapFunc:     PluginTables,
	}
	return p
}


func PluginTables(ctx context.Context, d *plugin.TableMapData) (map[string]*plugin.Table, error) {

	tables := map[string]*plugin.Table{}
	tables["http"] = tableData(ctx, d.Connection)

	return tables, nil

}
