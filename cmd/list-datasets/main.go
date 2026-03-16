package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/shanehull/yieldi/internal/satellite"
)

func main() {
	username := flag.String("username", "", "EarthExplorer username")
	token := flag.String("token", "", "Application token")
	flag.Parse()

	if *username == "" {
		*username = os.Getenv("USGS_M2M_USERNAME")
	}
	if *token == "" {
		*token = os.Getenv("USGS_M2M_TOKEN")
	}

	if *username == "" || *token == "" {
		log.Fatal("missing USGS_M2M_USERNAME or USGS_M2M_TOKEN")
	}

	client := satellite.NewLandsatClient(*username, *token)
	datasets, err := client.ListDatasets(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	for _, ds := range datasets {
		data, _ := json.MarshalIndent(ds, "", "  ")
		fmt.Println(string(data))
	}
}
