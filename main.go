package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type Jobs struct {
	Jobs []Job `json:"jobs"`
}

type Job struct {
	Project string  `json:"project"`
	ENV     string  `json:"env"`
	Port    int     `json:"port"`
	Role    string  `json:"role"`
	Entries []Entry `json:"entries"`
}

type Entry struct {
	HostName string `json:"hostname"`
	IP       string `json:"ip"`
}

type TCPCheckResult struct {
	Project  string
	ENV      string
	HostName string
	IP       string
	Port     int
	Role     string
	ReqTime  time.Duration
	Status   string
}

var TCPCheckResults = struct {
	sync.RWMutex
	m map[string]TCPCheckResult
}{m: make(map[string]TCPCheckResult)}

func main() {

	// read job info to memory when app start
	var checkList []Job

	confByte, err := ioutil.ReadFile("tcp_dial_check.conf")
	if err != nil {
		log.Fatalf("read config file fail, error: %s", err)
	}

	j := Jobs{}
	err = json.Unmarshal(confByte, &j)
	if err != nil {
		log.Fatalf("parse config file fail, error: %s", err)
	} else {
		for _, job := range j.Jobs {
			checkList = append(checkList, job)
		}
	}

	go func() {
		for {
			for _, job := range checkList {
				for _, entry := range job.Entries {
					addr := fmt.Sprintf("%s:%d", entry.IP, job.Port)
					tcr := TCPCheckResult{}
					tcr.Project = job.Project
					tcr.ENV = job.ENV
					tcr.HostName = entry.HostName
					tcr.IP = entry.IP
					tcr.Port = job.Port
					tcr.Role = job.Role
					go func() {
						reqTime, status, _ := tcpDialCheck(addr)
						tcr.ReqTime = reqTime
						tcr.Status = status

						TCPCheckResults.Lock()
						TCPCheckResults.m[addr] = tcr
						TCPCheckResults.Unlock()
					}()
				}
			}

			time.Sleep(12 * time.Second)
		}
	}()

	http.HandleFunc("/metrics", metrics)
	http.ListenAndServe(":9993", nil)
}

func tcpDialCheck(addr string) (reqTime time.Duration, status string, err error) {
	log.Printf("begin to check addr %q\n", addr)
	status = "bad"
	currentTime := time.Now()
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		log.Printf("finish check for addr %q, status: %s, error: %s\n", addr, status, err)
		return
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		log.Printf("finish check for addr %q, status: %s, error: %s\n", addr, status, err)
		return
	}

	reqTime = time.Now().Sub(currentTime)
	if n > 0 {
		status = "good"
	}
	log.Printf("finish check for addr %q, status: %s, time: %s\n", addr, status, reqTime)
	return
}

func metrics(w http.ResponseWriter, req *http.Request) {
	var metricsContents []string
	TCPCheckResults.RLock()
	for _, v := range TCPCheckResults.m {
		rStatus := fmt.Sprintf("tcp_dail_check_status{project=%q,env=%q,"+
			"hostname=%q,ip=%q,port=\"%d\",role=%q,status=%q} 1",
			v.Project,
			v.ENV,
			v.HostName,
			v.IP,
			v.Port,
			v.Role,
			v.Status)
		/*
			rTime := fmt.Sprintf("tcp_dail_check_total_time{project=%q,env=%q,"+
				"hostname=%q,ip=%q,port=\"%d\"} %d",
				v.Project,
				v.ENV,
				v.HostName,
				v.IP,
				v.Port,
				v.ReqTime.Nanoseconds())
		*/
		metricsContents = append(metricsContents, rStatus)
	}
	TCPCheckResults.RUnlock()

	metricContentsForShow := strings.Join(metricsContents, "\n")
	w.Write([]byte(metricContentsForShow))
}
