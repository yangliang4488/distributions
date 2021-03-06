package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sync"
	"time"
)

const ExportServerPort = ":3000"
const ExportServersUrl = "http://localhost" + ExportServerPort + "/services"

type registry struct {
	registations []Registration
	mutex        *sync.RWMutex
}

func (r *registry) add(reg Registration) error {
	log.Printf("添加服务 Add Service:%v with Url:%s\n", reg.ServiceName, reg.ServiceUrl)
	// 注册服务
	r.mutex.Lock()
	r.registations = append(r.registations, reg)
	r.mutex.Unlock()
	// 加载依赖的服务
	err := r.sendRequiredService(reg)
	if err != nil {
		return err
	}
	// 服务发现通知
	r.notify(patch{Added: []patchEntry{
		{
			Name: reg.ServiceName,
			Url:  reg.ServiceUrl,
		},
	}})
	return nil
}

func (r registry) notify(fullPatch patch) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	for _, reg := range r.registations {
		go func(reg Registration) {
			for _, reqSrvName := range reg.RequiredServices {
				p := new(patch)
				p.Added = []patchEntry{}
				p.Removed = []patchEntry{}

				sendUpdate := false

				for _, added := range fullPatch.Added {
					if added.Name == reqSrvName {
						p.Added = append(p.Added, added)
						sendUpdate = true
					}
				}
				for _, removed := range fullPatch.Removed {
					if removed.Name == reqSrvName {
						p.Removed = append(p.Removed, removed)
						sendUpdate = true
					}
				}
				// 发送通知
				if sendUpdate {
					err := r.sendPatch(*p, reg.ServiceUpdateUrl)
					if err != nil {
						log.Println(err)
						return
					}
				}
			}
		}(reg)
	}
}

func (r registry) sendRequiredService(reg Registration) error {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	var p patch
	for _, serviceReg := range r.registations {
		for _, serviceReq := range reg.RequiredServices {
			if serviceReg.ServiceName == serviceReq {
				p.Added = append(p.Added, patchEntry{
					Name: serviceReg.ServiceName,
					Url:  serviceReg.ServiceUrl,
				})
			}
		}
	}
	err := r.sendPatch(p, reg.ServiceUpdateUrl)
	if err != nil {
		return err
	}
	return nil
}

func (r registry) sendPatch(p patch, url string) error {
	pJson, err := json.Marshal(p)
	if err != nil {
		return err
	}
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(pJson))
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Failed to send patch with code:%v", resp.StatusCode)
	}
	return nil
}

func (r *registry) remove(url string) error {
	for i, srv := range reg.registations {
		if srv.ServiceUrl == url {
			reg.notify(patch{
				Removed: []patchEntry{
					{
						Name: srv.ServiceName,
						Url:  srv.ServiceUrl,
					},
				},
			})
			r.mutex.Lock()
			reg.registations = append(reg.registations[:i], r.registations[i+1:]...)
			r.mutex.Unlock()
		}
	}
	return nil
}

func (r registry) heartbeat(sec time.Duration) {
	for {
		var wg sync.WaitGroup
		for _, reg := range reg.registations {
			wg.Add(1)
			go func(reg Registration) {
				defer wg.Done()
				success := true
			loop:
				for attemps := 0; attemps < 3; attemps++ {
					resp, err := http.Get(reg.HeartbeatUrl)
					if resp.StatusCode == http.StatusOK {
						fmt.Printf("心跳检测 heartbeat check passed for service %v\n", reg.ServiceName)
						if !success {
							r.add(reg)
						}
						break loop
					}
					fmt.Printf("心跳检测 heartbeat check failed for service %v \n", reg.ServiceName)
					if err != nil {
						log.Println(err)
					}
					success = false
					r.remove(reg.ServiceUrl)
					time.Sleep(time.Second)
				}
			}(reg)
		}
		wg.Wait()
		time.Sleep(sec)
	}
}

func HandleHeartbeat() {
	var once sync.Once
	once.Do(func() {
		go reg.heartbeat(3 * time.Second)
	})
}

var reg = registry{
	registations: make([]Registration, 0),
	mutex:        new(sync.RWMutex),
}

type RegistrationService struct{}

func (s RegistrationService) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	log.Println("Request Received")
	switch r.Method {
	case http.MethodPost:
		dec := json.NewDecoder(r.Body)
		var r Registration
		err := dec.Decode(&r)
		if err != nil {
			log.Println(err)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
		// 服务注册
		err = reg.add(r)

		if err != nil {
			log.Println(err)
			rw.WriteHeader(http.StatusBadRequest)
			return
		}
	case http.MethodDelete:
		payload, err := ioutil.ReadAll(r.Body)
		if err != nil {
			log.Println(err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
		log.Printf("移除服务 Service with url:%s", string(payload))
		err = reg.remove(string(payload))
		if err != nil {
			log.Println(err)
			rw.WriteHeader(http.StatusInternalServerError)
			return
		}
	default:
		rw.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
}
