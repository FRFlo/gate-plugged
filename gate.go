package main

import (
	"github.com/minekube/gate-plugin-template/plugins/commands"
	"github.com/minekube/gate-plugin-template/plugins/detection"
	"github.com/minekube/gate-plugin-template/plugins/pelican"
	"github.com/minekube/gate-plugin-template/plugins/vanish"
	"go.minekube.com/gate/cmd/gate"
	"go.minekube.com/gate/pkg/edition/java/proxy"
)

// It's a normal Go program, we only need
// to register our plugins and execute Gate.
func main() {
	// Here we register our plugins with the proxy.
	proxy.Plugins = append(proxy.Plugins, pelican.Plugin)
	proxy.Plugins = append(proxy.Plugins, detection.Plugin)
	proxy.Plugins = append(proxy.Plugins, commands.Plugin)
	proxy.Plugins = append(proxy.Plugins, vanish.Plugin)

	// Simply execute Gate as if it was a normal Go program.
	// Gate will take care of everything else for us,
	// such as config auto-reloading and flags like --debug.
	gate.Execute()
}
