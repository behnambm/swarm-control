package main

import (
	"context"
	"flag"
	"github.com/docker/docker/client"
	"github.com/gorilla/mux"
	_ "github.com/mattn/go-sqlite3"
	"html/template"
	"log"
	"net/http"
	"time"
)

const (
	SecondsToSleepAfterUpScale = 30
	SecondsToCheckThreshold    = 10
	SecondsToCheckServices     = 3
)

func seedThreshold(cli *DockerController, db *SqliteDB) {
	services, srvErr := cli.getAllServices()
	if srvErr != nil {
		log.Panicln("can't seed - service error ", srvErr)
	}

	for _, service := range services {
		err := db.InsertThreshold(
			Threshold{
				ServiceID:        service.ID,
				HighMemThreshold: 80,
				LowMemThreshold:  20,
			},
		)
		if err != nil {
			log.Panicln("can't seed - set high to db", service.ID, err)
		}
	}
	log.Println("SEED SUCCESSFUL")
}

func main() {
	cli, cliErr := client.NewClientWithOpts(client.FromEnv)
	if cliErr != nil {
		log.Panicln("error connecting to docker: ", cliErr)
	}
	pingData, pingErr := cli.Ping(context.Background())
	if pingErr != nil {
		log.Panicln("Docker ping error: ", pingErr)
	}
	log.Println("CONNECTED TO DOCKER: ", pingData.APIVersion)

	databasePath := flag.String(
		"dbpath",
		"swarm.db",
		"Name of the SQLite database to store swarm data. e.g. swarm.db",
	)
	webAppPort := flag.String("port", "8090", "Port to use to start web app")

	flag.Parse()

	sqliteDB := NewSqliteDB(*databasePath)
	defer sqliteDB.db.Close()

	dockerControl := DockerController{cli: cli}
	controlServer := ControlServer{dc: &dockerControl, db: sqliteDB}

	// TODO: remove this and use a UI to update thresholds
	seedThreshold(&dockerControl, sqliteDB)

	tmpl := template.Must(template.ParseFiles("templates/index.gohtml"))
	router := mux.NewRouter()
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		services, srvErr := dockerControl.getAllServices()
		if srvErr != nil {
			log.Println("get services error: ", srvErr)
		}
		tmpl.Execute(w, services)
	})

	router.HandleFunc("/stream/services", controlServer.ServicesEventStreamHandler)
	router.HandleFunc("/{serviceId}/up", controlServer.ScaleUpHandler)
	router.HandleFunc("/{serviceId}/down", controlServer.ScaleDownHandler)
	router.HandleFunc("/{serviceId}/get", controlServer.GetServiceInfoHandler)

	go HandleThreshold(sqliteDB, &dockerControl)

	server := http.Server{
		Handler: router,
		Addr:    ":" + *webAppPort,
	}
	log.Fatal(server.ListenAndServe())
}

func HandleThreshold(db *SqliteDB, dc *DockerController) {
	for {
		serviceList, srvErr := db.GetAllServices()
		if srvErr != nil {
			time.Sleep(time.Second * SecondsToCheckThreshold)
			log.Println("get services error: ", srvErr)
		}
		if len(serviceList) == 0 {
			time.Sleep(time.Second * SecondsToCheckThreshold)

			continue
		}

		for _, service := range serviceList {
			memInfo, memErr := dc.getServiceMemoryInfo(service.ServiceID)
			if memErr != nil {
				log.Println("get mem info error: ", memErr)

				continue
			}
			if !memInfo.Unlimited && float64(service.HighMemThreshold) < memInfo.UsedPercentage() {
				upScaleErr := dc.UpScale(service.ServiceID)
				log.Println("Up scale - ", service.ServiceID)
				if upScaleErr != nil {
					log.Println("service: ", service.ServiceID, " - UP scale error: ", upScaleErr)
					break
				}
				time.Sleep(time.Second * SecondsToSleepAfterUpScale)
			}
		}

		time.Sleep(time.Second * SecondsToCheckThreshold)
	}
}
