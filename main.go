package main

import (
	"github.com/juanmill4/steampipe-plugin-darkeye/darkeye"
	"github.com/turbot/steampipe-plugin-sdk/v6/plugin"
)

func main() { plugin.Serve(&plugin.ServeOpts{PluginFunc: darkeye.Plugin}) }
