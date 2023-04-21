package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/gorilla/mux"
	"log"
	"net/http"
	"time"
)

type ControlServer struct {
	dc         *DockerController
	db         *SqliteDB
	ListenAddr string
}

func (s *ControlServer) ServicesEventStreamHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Cache-Control", "no-store")
	w.Header().Add("Content-Type", "text/event-stream")

	for {
		select {
		case <-time.After(time.Second * SecondsToCheckServices):

			fmt.Fprintf(w, "event: services\n")
			ss := time.Now()
			services, dErr := s.dc.getAllServices()
			for i := 0; i < len(services); i++ {
				srvInfo, thresholdErr := s.db.GetServiceInfo(services[i].ID)
				if thresholdErr != nil {
					log.Println("getting threshold info error: ", thresholdErr)

					continue
				}
				services[i].LowMemThreshold = srvInfo.LowMemThreshold
				services[i].HighMemThreshold = srvInfo.HighMemThreshold
			}
			fmt.Println("total time getting services: ", time.Now().Sub(ss))
			if dErr != nil {
				log.Panicln("getting services: ", dErr)
			}
			data, err := json.Marshal(services)
			if err != nil {
				log.Println("marshal error: ", err)
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
			// Client has disconnected, exit the loop
			return
		}
	}
}

func (s *ControlServer) ScaleUpHandler(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["serviceId"]
	if err := s.dc.UpScale(serviceID); err != nil {
		log.Println("ERR UP SCALE HANDLER - ", err)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *ControlServer) ScaleDownHandler(w http.ResponseWriter, r *http.Request) {
	serviceID := mux.Vars(r)["serviceId"]
	service, _, err := s.dc.cli.ServiceInspectWithRaw(context.Background(), serviceID, types.ServiceInspectOptions{})
	if err != nil {
		log.Println("getting service to sale up error: ", err)
	}
	newReplication := *(service.Spec.Mode.Replicated.Replicas) - 1
	if newReplication <= 0 {
		fmt.Println("can't scale to zero")

		return
	}
	service.Spec.Mode.Replicated.Replicas = &newReplication
	_, err = s.dc.cli.ServiceUpdate(context.Background(), serviceID, service.Version, service.Spec, types.ServiceUpdateOptions{})
	if err != nil {
		log.Println("service update error: ", err)
	}
}

func (s *ControlServer) GetServiceInfoHandler(w http.ResponseWriter, r *http.Request) {
	serviceId := mux.Vars(r)["serviceId"]
	thresh, err := s.db.GetServiceInfo(serviceId)
	if err != nil {
		log.Println("err : ", err)

		return
	}
	fmt.Println("data: ", thresh)
}
