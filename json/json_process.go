package main

import (
	"encoding/json"
	"fmt"
	"log"
)

func newBool(b bool) *bool {
	return &b
}

func newInt(b int) *int {
	return &b
}

type Name struct {
	Service string `json:"service,omitempty"`
	Method  string `json:"method,omitempty"`
}

type MC struct {
	Name                    *Name  `json:"name,omitempty"`
	WaitForReady            *bool  `json:"waitForReady,omitempty"`
	Timeout                 string `json:"timeout,omitempty"`
	MaxRequestMessageBytes  *int   `json:"maxRequestMessageBytes,omitempty"`
	MaxResponseMessageBytes *int   `json:"maxResponseMessageBytes,omitempty"`
}

type SC struct {
	LoadBalancingPolicy string `json:"loadBalancingPolicy,omitempty"`
	MethodConfig        *MC    `json:"methodConfig,omitempty"`
}

type Choice struct {
	ClientLanguage []string `json:"clientLanguage,omitempty"`
	Percentage     *int     `json:"percentage,omitempty"`
	ClientHostName []string `json:"clientHostName,omitempty"`
	ServiceConfig  *SC      `json:"serviceConfig,omitempty"`
}

type RawChoice struct {
	ClientLanguage []string
	Percentage     *int
	ClientHostName []string
	ServiceConfig  json.RawMessage
}

var (
	scs = []*SC{
		&SC{
			LoadBalancingPolicy: "round_robin",
			MethodConfig: &MC{
				Name: &Name{
					Service: "foo",
					Method:  "bar",
				},
				WaitForReady: newBool(true),
			},
		},
		&SC{
			LoadBalancingPolicy: "grpclb",
			MethodConfig: &MC{
				Name: &Name{
					Service: "all",
				},
				Timeout: "1s",
			},
		},
		&SC{
			MethodConfig: &MC{
				Name: &Name{
					Method: "bar",
				},
				MaxRequestMessageBytes:  newInt(1024),
				MaxResponseMessageBytes: newInt(1024),
			},
		},
		&SC{
			MethodConfig: &MC{
				Name: &Name{
					Service: "foo",
					Method:  "bar",
				},
				WaitForReady:            newBool(true),
				Timeout:                 "1s",
				MaxRequestMessageBytes:  newInt(1024),
				MaxResponseMessageBytes: newInt(1024),
			},
		},
	}
)

func containsString(a []string, b string) bool {
	if len(a) == 0 {
		return true
	}
	for _, c := range a {
		if c == b {
			return true
		}
	}
	return false
}

func chosenByPercentage(a *int, b int) bool {
	if a == nil {
		return true
	}
	if *a == 0 {
		return false
	}
	return true
}

func main() {
	var sc []*Choice
	sc = append(sc, &Choice{ClientLanguage: []string{"CPP", "JAVA"}, ServiceConfig: scs[0]})
	sc = append(sc, &Choice{Percentage: newInt(0), ServiceConfig: scs[1]})
	sc = append(sc, &Choice{ClientHostName: []string{"localhost"}, ServiceConfig: scs[2]})
	sc = append(sc, &Choice{ServiceConfig: scs[3]})
	b, err := json.Marshal(sc)
	if err != nil {
		fmt.Println("error dumping json string")
		return
	}
	jsStr := string(b)
	fmt.Println("============ json string ============\n", jsStr)

	/* unmarshall json string */
	var rcs []RawChoice
	err = json.Unmarshal([]byte(jsStr), &rcs)
	if err != nil {
		log.Fatalln("error:", err)
	}

	for i, c := range rcs {
		if !containsString(c.ClientLanguage, "GO") ||
			!containsString(c.ClientHostName, "lala") ||
			!chosenByPercentage(c.Percentage, 1) {
			fmt.Println(!containsString(c.ClientLanguage, "GO"), !containsString(c.ClientHostName, "lala"), !chosenByPercentage(c.Percentage, 1), i)
			continue
		}
		fmt.Println("============ service config str ============")
		fmt.Println(string(c.ServiceConfig))
		break
	}

}
