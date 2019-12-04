package main

import (
	"net/http"
	"net/url"
	"log"
	"time"
	"github.com/kelseyhightower/envconfig"
	"io/ioutil"
)

type ConfigProxy struct {
	ProxyUrl string `envconfig:"HTTP_PROXY"`
}

func getProxy() string {
	var cfg ConfigProxy
	err := envconfig.Process("", &cfg)
	if err != nil {
		return ""
	}
	return cfg.ProxyUrl
}

func setClient(proxy string, t time.Duration) *http.Client {
	if proxy == "" {
		return &http.Client{}
	} else {
		proxyURL, err := url.Parse(proxy)
		if err != nil {
			log.Println(err)
		}

		transport := &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		}

		client := &http.Client{
			Transport: transport,
			Timeout:   time.Second * t,
		}
		return client
	}

}

func getPage(url string) ([]byte, error) {
	client := setClient(getProxy(), 2)
	resp, err := client.Get(url)
	if resp != nil {
		defer resp.Body.Close()
		if err != nil {
			log.Println("error request url", url)
		}
		b, err := ioutil.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			log.Printf("Сбой запроса: %s  %s\n", resp.Status, url)
		}
		if err != nil {
			log.Println("Error from body")
		}
		b = []byte("Found")
		return b, err
		//fmt.Println("response URL",url)
	} else {
		return []byte("Not found"), err
	}

}
