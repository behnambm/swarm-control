package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/swarm"
	"github.com/docker/docker/client"
	"log"
	"time"
)

type ServiceInfo struct {
	ID               string
	Name             string
	Replicas         uint64
	Image            string
	EndpointPorts    []string
	MemInfo          *MemoryInfo
	LowMemThreshold  int
	HighMemThreshold int
}

type DockerController struct {
	cli *client.Client
}

func (dc *DockerController) getAllServices() ([]ServiceInfo, error) {
	services, err := dc.cli.ServiceList(context.Background(), types.ServiceListOptions{})
	if err != nil {
		return nil, err
	}
	fmt.Println("number of services to loop : ", len(services))
	var serviceList []ServiceInfo
	serviceInfoChan := make(chan ServiceInfo)
	for _, serviceItem := range services {
		go func(service swarm.Service) {
			var endpoints []string
			for _, port := range service.Endpoint.Ports {
				endpoints = append(endpoints, fmt.Sprintf("%d/%s", port.PublishedPort, port.Protocol))
			}
			s := time.Now()
			memInfo, memErr := dc.getServiceMemoryInfo(service.ID)
			if memErr != nil {
				log.Println("error getting mem info: ", memErr)
			}
			fmt.Println("time took to get mem info for service ", service.Spec.Name, ": ", time.Now().Sub(s))
			serviceInfoChan <- ServiceInfo{
				ID:            service.ID,
				Name:          service.Spec.Name,
				Replicas:      *service.Spec.Mode.Replicated.Replicas,
				Image:         service.Spec.TaskTemplate.ContainerSpec.Image,
				EndpointPorts: endpoints,
				MemInfo:       memInfo,
			}
		}(serviceItem)
	}

	for i := 0; i < len(services); i++ {
		serviceInfo := <-serviceInfoChan
		serviceList = append(serviceList, serviceInfo)
	}
	return serviceList, nil
}

//
//func (dc *DockerController) UpScale(serviceID string) error {
//	service, _, err := dc.cli.ServiceInspectWithRaw(context.Background(), serviceID, types.ServiceInspectOptions{})
//	if err != nil {
//		log.Println("getting service to sale up error: ", err)
//		return err
//	}
//	newReplication := *(service.Spec.Mode.Replicated.Replicas) + 1
//	service.Spec.Mode.Replicated.Replicas = &newReplication
//	ctx := context.Background()
//	fmt.Println("\t\t up scale ")
//	_, err = dc.cli.ServiceUpdate(ctx, serviceID, service.Version, service.Spec, types.ServiceUpdateOptions{})
//	if err != nil {
//		log.Println("service update error: ", err)
//	}
//	fmt.Println("waiting to finish: ", time.Now().Unix())
//	<-ctx.Done()
//	fmt.Println("after waiting to finish: ", time.Now().Unix())
//	return nil
//}

func (dc *DockerController) UpScale(serviceID string) error {
	service, _, err := dc.cli.ServiceInspectWithRaw(context.Background(), serviceID, types.ServiceInspectOptions{})
	if err != nil {
		log.Println("getting service error: ", err)

		return err
	}

	newReplication := *(service.Spec.Mode.Replicated.Replicas) + 1
	service.Spec.Mode.Replicated.Replicas = &newReplication

	ctx := context.Background()
	_, updateErr := dc.cli.ServiceUpdate(ctx, serviceID, service.Version, service.Spec, types.ServiceUpdateOptions{})
	if updateErr != nil {
		log.Println("service update error: ", updateErr)

		return updateErr
	}

	return nil
}

func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

type MemoryInfo struct {
	Available uint64 `json:"available"`
	Used      uint64 `json:"used"`
	Unlimited bool   `json:"unlimited"`
}

func (m *MemoryInfo) UsedPercentage() float64 {
	if m.Available == 0 {
		return 0.0
	}
	return float64(m.Used) / float64(m.Available) * 100
}

func getServiceMemoryLimit(resources *swarm.ResourceRequirements) uint64 {
	if resources == nil || resources.Limits == nil || resources.Limits.MemoryBytes == 0 {
		return 0
	}

	return uint64(resources.Limits.MemoryBytes)
}

func (dc *DockerController) getServiceMemoryInfo(serviceID string) (*MemoryInfo, error) {
	service, _, err := dc.cli.ServiceInspectWithRaw(context.Background(), serviceID, types.ServiceInspectOptions{})
	if err != nil {
		return nil, err
	}

	numberOfReplicas := *(service.Spec.Mode.Replicated.Replicas)
	serviceMemory := getServiceMemoryLimit(service.Spec.TaskTemplate.Resources)
	available := numberOfReplicas * serviceMemory

	// Get the used memory for the service
	tasks, err := dc.cli.TaskList(context.Background(), types.TaskListOptions{
		Filters: filters.NewArgs(
			filters.Arg("service", serviceID),
			filters.Arg("desired-state", "running"),
		),
	})
	if err != nil {
		return nil, err
	}
	used := uint64(0)
	memChan := make(chan uint64)
	for _, taskItem := range tasks {
		go func(task swarm.Task) {
			if task.Status.ContainerStatus == nil {
				log.Println("error task goroutine : no container status")
				memChan <- 0

				return
			}
			taskStats, statsErr := dc.cli.ContainerStats(context.Background(), task.Status.ContainerStatus.ContainerID, false)
			if statsErr != nil {
				log.Println("error task goroutine - container stats error: ", statsErr)
				memChan <- 0

				return
			}
			defer taskStats.Body.Close()
			var stats types.StatsJSON
			err = json.NewDecoder(taskStats.Body).Decode(&stats)
			if err != nil {
				log.Println("error task goroutine: ", err)
				memChan <- 0

				return
			}

			memChan <- stats.MemoryStats.Usage
		}(taskItem)
	}
	// todo: it's better to use wait group instead of waiting for the length of tasks
	for i := 0; i < len(tasks); i++ {
		usedMem := <-memChan
		used += usedMem
	}
	// Create a MemoryInfo struct and return it
	memoryInfo := MemoryInfo{
		Available: available,
		Used:      used,
	}
	if available == 0 {
		memoryInfo.Unlimited = true
	}
	return &memoryInfo, nil
}
