package main

import (
	"net/http"
	"net/url"
	"strings"
)

func particleRequest(device string, command string, arg string, accessToken string) error {
	apiEndpoint := "https://api.particle.io/v1/devices/" + device + "/" + command

	urlValues := url.Values{}
	urlValues.Add("access_token", accessToken)
	urlValues.Add("arg", arg)

	req, _ := http.NewRequest("POST", apiEndpoint, strings.NewReader(urlValues.Encode()))
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")

	httpClient := http.Client{}
	_, err := httpClient.Do(req)

	return err
}
