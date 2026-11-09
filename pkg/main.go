package main

import (
	"os"

	"github.com/grafana/grafana-plugin-sdk-go/backend/app"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"

	"github.com/mlimarenko/grafana-problem-desk/pkg/plugin"
)

func main() {
	if err := app.Manage("mlimarenko-problemdesk-app", plugin.NewAppInstance, app.ManageOpts{}); err != nil {
		log.DefaultLogger.Error(err.Error())
		os.Exit(1)
	}
}
