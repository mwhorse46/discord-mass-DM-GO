// Copyright (C) 2021 github.com/V4NSH4J
//
// This source code has been released under the GNU Affero General Public
// License v3.0. A copy of this license is available at
// https://www.gnu.org/licenses/agpl-3.0.en.html

package instance

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/V4NSH4J/discord-mass-dm-GO/utilities"
)

func (in *Instance) SolveCaptcha(sitekey string, cookie string, rqData string, rqToken string, url string) (string, error) {
	switch true {
	case in.Config.CaptchaSettings.Self != "":
		return in.self(sitekey, rqData)
	case in.Config.CaptchaSettings.CaptchaAPI == "invisifox.com":
		return in.invisifox(sitekey, cookie, rqData)
	case in.Config.CaptchaSettings.CaptchaAPI == "captchaai.io":
		return in.captchaAI(sitekey, rqData)
	case utilities.Contains([]string{"capmonster.cloud", "anti-captcha.com"}, in.Config.CaptchaSettings.CaptchaAPI):
		return in.Capmonster(sitekey, url, rqData, cookie)
	case utilities.Contains([]string{"2captcha.com", "rucaptcha.com"}, in.Config.CaptchaSettings.CaptchaAPI):
		return in.twoCaptcha(sitekey, rqData, url)
	case in.Config.CaptchaSettings.CaptchaAPI == "capcat.xyz":
		return in.CapCat(sitekey, rqData)
	default:
		return "", fmt.Errorf("unsupported captcha api: %s", in.Config.CaptchaSettings.CaptchaAPI)
	}
}

/*
	2Captcha/RuCaptcha
*/

func (in *Instance) twoCaptcha(sitekey, rqdata, site string) (string, error) {
	var solvedKey string
	inEndpoint := "https://2captcha.com/in.php"
	inURL, err := url.Parse(inEndpoint)
	if err != nil {
		return solvedKey, fmt.Errorf("error while parsing url %v", err)
	}
	q := inURL.Query()
	if in.Config.CaptchaSettings.ClientKey == "" {
		return solvedKey, fmt.Errorf("client key is empty")
	}
	q.Set("key", in.Config.CaptchaSettings.ClientKey)
	q.Set("method", "hcaptcha")
	q.Set("sitekey", sitekey)
	// Page URL same as referer in headers
	q.Set("pageurl", "https://discord.com")
	q.Set("userAgent", in.UserAgent)
	q.Set("json", "1")
	q.Set("soft_id", "3359")
	if rqdata != "" {
		q.Set("data", rqdata)
		q.Set("invisible", "0")
	}
	if in.Config.ProxySettings.ProxyForCaptcha {
		q.Set("proxy", in.Proxy)
		q.Set("proxytype", "http")
	}
	inURL.RawQuery = q.Encode()
	if in.Config.CaptchaSettings.CaptchaAPI == "2captcha.com" {
		inURL.Host = "2captcha.com"
	} else if in.Config.CaptchaSettings.CaptchaAPI == "rucaptcha.com" {
		inURL.Host = "rucaptcha.com"
	}
	inEndpoint = inURL.String()
	req, err := http.NewRequest(http.MethodGet, inEndpoint, nil)
	if err != nil {
		return solvedKey, fmt.Errorf("error creating request [%v]", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return solvedKey, fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return solvedKey, fmt.Errorf("error reading response [%v]", err)
	}
	var inResponse twoCaptchaSubmitResponse
	err = json.Unmarshal(body, &inResponse)
	if err != nil {
		return solvedKey, fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if inResponse.Status != 1 {
		return solvedKey, fmt.Errorf("error %v", inResponse.Request)
	}
	outEndpoint := "https://2captcha.com/res.php"
	outURL, err := url.Parse(outEndpoint)
	if err != nil {
		return solvedKey, fmt.Errorf("error while parsing url %v", err)
	}
	in.LastIDstr = inResponse.Request
	q = outURL.Query()
	q.Set("key", in.Config.CaptchaSettings.ClientKey)
	q.Set("action", "get")
	q.Set("id", inResponse.Request)
	q.Set("json", "1")
	if in.Config.CaptchaSettings.CaptchaAPI == "2captcha.com" {
		outURL.Host = "2captcha.com"
	} else if in.Config.CaptchaSettings.CaptchaAPI == "rucaptcha.com" {
		outURL.Host = "rucaptcha.com"
	}
	outURL.RawQuery = q.Encode()
	outEndpoint = outURL.String()

	time.Sleep(10 * time.Second)
	now := time.Now()
	for {
		if time.Since(now) > time.Duration(in.Config.CaptchaSettings.Timeout)*time.Second {
			return solvedKey, fmt.Errorf("captcha response from 2captcha timedout")
		}
		req, err = http.NewRequest(http.MethodGet, outEndpoint, nil)
		if err != nil {
			return solvedKey, fmt.Errorf("error creating request [%v]", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return solvedKey, fmt.Errorf("error sending request [%v]", err)
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return solvedKey, fmt.Errorf("error reading response [%v]", err)
		}
		var outResponse twoCaptchaSubmitResponse
		err = json.Unmarshal(body, &outResponse)
		if err != nil {
			return solvedKey, fmt.Errorf("error unmarshalling response [%v]", err)
		}
		if outResponse.Request == "CAPCHA_NOT_READY" {
			time.Sleep(5 * time.Second)
			continue
		} else if strings.Contains(string(body), "ERROR") {
			return solvedKey, fmt.Errorf("error %v", outResponse.Request)
		} else {
			solvedKey = outResponse.Request
			break
		}
	}
	return solvedKey, nil
}

/*
	Capmonster
*/

func (in *Instance) Capmonster(sitekey, website, rqdata, cookies string) (string, error) {
	var solvedKey string
	inEndpoint, outEndpoint := fmt.Sprintf("https://api.%s/createTask", in.Config.CaptchaSettings.CaptchaAPI), fmt.Sprintf("https://api.%s/getTaskResult", in.Config.CaptchaSettings.CaptchaAPI)
	var submitCaptcha CapmonsterPayload
	if in.Config.CaptchaSettings.ClientKey == "" {
		return solvedKey, fmt.Errorf("no client key provided in config")
	} else {
		submitCaptcha.ClientKey = in.Config.CaptchaSettings.ClientKey
	}
	if in.Config.CaptchaSettings.CaptchaAPI == "anti-captcha.com" {
		submitCaptcha.SoftID = 1021
	}
	if in.Config.ProxySettings.ProxyForCaptcha && in.Proxy != "" {
		submitCaptcha.Task.CaptchaType = "HCaptchaTask"
		if strings.Contains(in.Proxy, "@") {
			// User:pass authenticated proxy
			parts := strings.Split(in.Proxy, "@")
			userPass, ipPort := parts[0], parts[1]
			if !strings.Contains(ipPort, ":") || !strings.Contains(userPass, ":") {
				return solvedKey, fmt.Errorf("invalid proxy format")
			}
			submitCaptcha.Task.ProxyType = "http"
			submitCaptcha.Task.ProxyLogin, submitCaptcha.Task.ProxyPassword = strings.Split(userPass, ":")[0], strings.Split(userPass, ":")[1]
			port := strings.Split(ipPort, ":")[1]
			var err error
			submitCaptcha.Task.ProxyPort, err = strconv.Atoi(port)
			if err != nil {
				return solvedKey, fmt.Errorf("invalid proxy format")
			}
			submitCaptcha.Task.ProxyAddress = strings.Split(ipPort, ":")[0]
		} else {
			if !strings.Contains(in.Proxy, ":") {
				return solvedKey, fmt.Errorf("invalid proxy format")
			}
			submitCaptcha.Task.ProxyAddress = strings.Split(in.Proxy, ":")[0]
			port := strings.Split(in.Proxy, ":")[1]
			var err error
			submitCaptcha.Task.ProxyPort, err = strconv.Atoi(port)
			if err != nil {
				return solvedKey, fmt.Errorf("invalid proxy format")
			}
		}
	} else {
		submitCaptcha.Task.CaptchaType = "HCaptchaTaskProxyless"
	}
	submitCaptcha.Task.WebsiteURL, submitCaptcha.Task.WebsiteKey, submitCaptcha.Task.UserAgent = "https://discord.com", sitekey, in.UserAgent
	if rqdata != "" && in.Config.CaptchaSettings.CaptchaAPI == "capmonster.cloud" {
		submitCaptcha.Task.Data = rqdata
		// Try with true too
		submitCaptcha.Task.IsInvisible = true
	} else if rqdata != "" && in.Config.CaptchaSettings.CaptchaAPI == "anti-captcha.com" {
		submitCaptcha.Task.IsInvisible = false
		submitCaptcha.Task.Enterprise.RqData = rqdata
		submitCaptcha.Task.Enterprise.Sentry = true
	}
	payload, err := json.Marshal(submitCaptcha)
	if err != nil {
		return solvedKey, fmt.Errorf("error while marshalling payload %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, inEndpoint, strings.NewReader(string(payload)))
	if err != nil {
		return solvedKey, fmt.Errorf("error creating request [%v]", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return solvedKey, fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return solvedKey, fmt.Errorf("error reading response [%v]", err)
	}
	var inResponse CapmonsterSubmitResponse
	err = json.Unmarshal(body, &inResponse)
	if err != nil {
		return solvedKey, fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if inResponse.ErrorID != 0 {
		return solvedKey, fmt.Errorf("error %v %v", inResponse.ErrorID, string(body))
	}
	var retrieveCaptcha CapmonsterPayload
	retrieveCaptcha.ClientKey = in.Config.CaptchaSettings.ClientKey
	retrieveCaptcha.TaskId = inResponse.TaskID
	in.LastID = inResponse.TaskID
	payload, err = json.Marshal(retrieveCaptcha)
	if err != nil {
		return solvedKey, fmt.Errorf("error while marshalling payload %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	time.Sleep(5 * time.Second)
	t := time.Now()
	for i := 0; i < 120; i++ {
		if time.Since(t).Seconds() >= float64(in.Config.CaptchaSettings.Timeout) {
			return solvedKey, fmt.Errorf("timedout - increase timeout in config to wait longer")
		}
		req, err = http.NewRequest(http.MethodPost, outEndpoint, bytes.NewBuffer(payload))
		if err != nil {
			return solvedKey, fmt.Errorf("error creating request [%v]", err)
		}
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return solvedKey, fmt.Errorf("error sending request [%v]", err)
		}
		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return solvedKey, fmt.Errorf("error reading response [%v]", err)
		}
		var outResponse CapmonsterOutResponse
		err = json.Unmarshal(body, &outResponse)
		if err != nil {
			return solvedKey, fmt.Errorf("error unmarshalling response [%v]", err)
		}
		if outResponse.ErrorID != 0 {
			return solvedKey, fmt.Errorf("error %v %v", outResponse.ErrorID, string(body))
		}
		if outResponse.Status == "ready" {
			solvedKey = outResponse.Solution.CaptchaResponse
			break
		} else if outResponse.Status == "processing" {
			time.Sleep(5 * time.Second)
			continue
		} else {
			return solvedKey, fmt.Errorf("error invalid status %v %v", outResponse.ErrorID, string(body))
		}

	}
	return solvedKey, nil
}

func (in *Instance) ReportIncorrectRecaptcha() error {
	site := "https://api.anti-captcha.com/reportIncorrectHcaptcha"
	payload := CapmonsterPayload{
		ClientKey: in.Config.CaptchaSettings.ClientKey,
		TaskId:    in.LastID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("error while marshalling payload %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, site, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return fmt.Errorf("error creating request [%v]", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("error reading response [%v]", err)
	}
	var outResponse CapmonsterOutResponse
	err = json.Unmarshal(body, &outResponse)
	if err != nil {
		return fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if outResponse.Status != "success" {
		return fmt.Errorf("error %v ", outResponse.ErrorID)
	}

	return nil
}

func (in *Instance) CapCat(sitekey, rqdata string) (string, error) {
	postURL := "http://capcat.xyz/api/tasks"
	x := CapCat{
		SiteKey: sitekey,
		RqData:  rqdata,
		ApiKey:  in.Config.CaptchaSettings.ClientKey,
	}
	ipAPI := "https://api.myip.com"
	req, err := http.NewRequest("GET", ipAPI, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request [%v]", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response [%v]", err)
	}
	if !strings.Contains(string(body), "ip") {
		return "", fmt.Errorf("error invalid response [%v]", string(body))
	}
	var ipResponse map[string]interface{}
	err = json.Unmarshal(body, &ipResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response [%v]", err)
	}
	x.IP = ipResponse["ip"].(string)
	payload, err := json.Marshal(x)
	if err != nil {
		return "", fmt.Errorf("error while marshalling payload %v", err)
	}
	req, err = http.NewRequest(http.MethodPost, postURL, strings.NewReader(string(payload)))
	if err != nil {
		return "", fmt.Errorf("error creating request [%v]", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response [%v]", err)
	}
	var outResponse CapCatResponse
	err = json.Unmarshal(body, &outResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if outResponse.ID == 0 {
		return "", fmt.Errorf("error %v %v", outResponse.Msg, string(body))
	}
	t := time.Now()
	for {
		time.Sleep(5 * time.Second)
		if time.Since(t).Seconds() >= float64(in.Config.CaptchaSettings.Timeout) || time.Since(t).Seconds() >= 300 {
			return "", fmt.Errorf("timedout - increase timeout in config to wait longer")
		}
		getURL := "http://capcat.xyz/api/result/"
		y := CapCat{
			ID:     fmt.Sprintf("%v", outResponse.ID),
			ApiKey: in.Config.CaptchaSettings.ClientKey,
		}
		payload, err = json.Marshal(y)
		if err != nil {
			return "", fmt.Errorf("error while marshalling payload %v", err)
		}
		req, err = http.NewRequest(http.MethodPost, getURL, strings.NewReader(string(payload)))
		if err != nil {
			return "", fmt.Errorf("error creating request [%v]", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("error sending request [%v]", err)
		}
		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("error reading response [%v]", err)
		}
		var outResponse CapCatResponse
		err = json.Unmarshal(body, &outResponse)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling response [%v]", err)
		}
		if strings.Contains(string(body), "working") {
			continue
		} else if outResponse.Code == 1 && outResponse.Data != "" {
			return outResponse.Data, nil
		} else {
			return "", fmt.Errorf("error %v", string(body))
		}
	}
}

func (in *Instance) self(sitekey, rqData string) (string, error) {
	var solution string
	var err error
	link := in.Config.CaptchaSettings.Self
	if link == "" {
		return "", fmt.Errorf("self captcha not configured")
	}
	client := http.Client{
		Timeout: 30 * time.Second,
	}
	selfPayload := SelfRequest{
		Sitekey:   sitekey,
		RqData:    rqData,
		Host:      "discord.com",
		Proxy:     in.Proxy,
		Username:  in.Config.CaptchaSettings.SelfUsername,
		Password:  in.Config.CaptchaSettings.SelfPassword,
		ProxyType: "http",
	}
	payloadBytes, err := json.Marshal(selfPayload)
	if err != nil {
		return "", fmt.Errorf("error marshalling payload %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, link, bytes.NewReader(payloadBytes))
	if err != nil {
		return "", fmt.Errorf("error creating request [%v]", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response [%v]", err)
	}
	var outResponse SelfResponse
	err = json.Unmarshal(body, &outResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if outResponse.Answer != "" {
		solution = outResponse.Answer
	} else {
		return "", fmt.Errorf("error %v", string(body))
	}
	return solution, err
}

type CapCat struct {
	ApiKey  string `json:"apikey"`
	SiteKey string `json:"sitkey"`
	RqData  string `json:"rqdata"`
	IP      string `json:"ip"`
	ID      string `json:"id,omitempty"`
}

type CapCatResponse struct {
	ID   int    `json:"id,omitempty"`
	Msg  string `json:"mess,omitempty"`
	Code int    `json:"code,omitempty"`
	Data string `json:"data,omitempty"`
}

type SelfRequest struct {
	Sitekey   string `json:"sitekey"`
	RqData    string `json:"rqdata"`
	Proxy     string `json:"proxy"`
	Host      string `json:"host"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	ProxyType string `json:"proxytype"`
}

type SelfResponse struct {
	Answer string `json:"generated_pass_UUID"`
}

/*
	invisifox
*/

func (in *Instance) invisifox(sitekey, cookie, rqdata string) (string, error) {
	var solvedKey string
	inEndpoint := fmt.Sprintf(`https://api.%s/hcaptcha`, in.Config.CaptchaSettings.CaptchaAPI)
	inURL, err := url.Parse(inEndpoint)
	if err != nil {
		return solvedKey, fmt.Errorf("error while parsing url %v", err)
	}
	q := inURL.Query()
	if in.Config.CaptchaSettings.ClientKey == "" {
		return solvedKey, fmt.Errorf("client key is empty")
	}
	if in.Proxy == "" {
		return solvedKey, fmt.Errorf("proxies are mandatory with this API. turn on proxy_for_captcha in config")
	}
	q.Set("token", in.Config.CaptchaSettings.ClientKey)
	q.Set("siteKey", sitekey)
	q.Set("pageurl", "discord.com")
	q.Set("proxy", in.Proxy)
	q.Set("useragent", in.UserAgent)
	q.Set("cookies", cookie)
	q.Set("invisible", "false")
	if rqdata != "" {
		q.Set("rqdata", rqdata)
	}
	inURL.RawQuery = q.Encode()
	inEndpoint = inURL.String()
	req, err := http.NewRequest(http.MethodGet, inEndpoint, nil)
	if err != nil {
		return solvedKey, fmt.Errorf("error creating request [%v]", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return solvedKey, fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return solvedKey, fmt.Errorf("error reading response [%v]", err)
	}
	var outResponse invisifoxSubmitResponse
	err = json.Unmarshal(body, &outResponse)
	if err != nil {
		return solvedKey, fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if outResponse.Status != "OK" {
		return solvedKey, fmt.Errorf("error %v", string(body))
	}
	taskID := outResponse.TaskID
	time.Sleep(25 * time.Second)
	t := time.Now()
	for {
		if int(time.Since(t).Seconds()) > in.Config.CaptchaSettings.Timeout {
			return solvedKey, fmt.Errorf("timedout while waiting for captcha to be solved, increase timeout in config to wait longer")
		}
		outEndpoint := fmt.Sprintf(`https://api.%s/solution`, in.Config.CaptchaSettings.CaptchaAPI)
		outURL, err := url.Parse(outEndpoint)
		if err != nil {
			return solvedKey, fmt.Errorf("error while parsing url %v", err)
		}
		q := outURL.Query()
		q.Set("token", in.Config.CaptchaSettings.ClientKey)
		q.Set("taskId", taskID)
		outURL.RawQuery = q.Encode()
		outEndpoint = outURL.String()
		req, err := http.NewRequest(http.MethodGet, outEndpoint, nil)
		if err != nil {
			return solvedKey, fmt.Errorf("error creating request [%v]", err)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return solvedKey, fmt.Errorf("error sending request [%v]", err)
		}
		defer resp.Body.Close()
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return solvedKey, fmt.Errorf("error reading response [%v]", err)
		}
		var outResponse invisifoxSolutionResponse
		err = json.Unmarshal(body, &outResponse)
		if err != nil {
			return solvedKey, fmt.Errorf("error unmarshalling response [%v]", err)
		}
		if outResponse.Status == "OK" {
			solvedKey = outResponse.Solution
			break
		} else if outResponse.Status == "WAITING" {
			time.Sleep(5 * time.Second)
			continue
		} else {
			return solvedKey, fmt.Errorf("error %v", string(body))
		}
	}
	return solvedKey, err
}

type invisifoxSubmitResponse struct {
	Status string `json:"status"`
	TaskID string `json:"taskId"`
}
type invisifoxSolutionResponse struct {
	Status   string `json:"status"`
	Solution string `json:"solution"`
}

/*
	invisifox
*/

func (in *Instance) captchaAI(sitekey, rqdata string) (string, error) {
	postEndpoint := fmt.Sprintf(`https://api.superai.pro/api/v1/task?apiKey=%s`, in.Config.CaptchaSettings.ClientKey)
	hCaptchaType := "HCaptchaV1"
	if rqdata != "" {
		hCaptchaType += "Enterprise"
	}
	if !in.Config.ProxySettings.ProxyForCaptcha {
		hCaptchaType += "Proxyless"
	}
	p := captchaAISubmit{
		Type:      hCaptchaType,
		SiteKey:   sitekey,
		PageURL:   "https://discord.com",
		Proxy:     in.ProxyProt,
		UserAgent: in.UserAgent,
		Timeout:   120,
		RqData:    rqdata,
	}
	pBytes, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("error marshalling captchaAI request [%v]", err)
	}
	fmt.Println(string(pBytes))
	req, err := http.NewRequest(http.MethodPost, postEndpoint, bytes.NewReader(pBytes))
	if err != nil {
		return "", fmt.Errorf("error creating request [%v]", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("error sending request [%v]", err)
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response [%v]", err)
	}
	fmt.Println(string(body))
	var submitResponse captchaAISubmitResponse
	err = json.Unmarshal(body, &submitResponse)
	if err != nil {
		return "", fmt.Errorf("error unmarshalling response [%v]", err)
	}
	if submitResponse.ID == "" {
		return "", fmt.Errorf("error %v", string(body))
	}
	t := submitResponse.ID
	now := time.Now()
	for {
		if time.Since(now).Seconds() > 120 {
			break
		}
		time.Sleep(15 * time.Second)
		resultEndpoint := fmt.Sprintf(`https://api.superai.pro/api/v1/task?apiKey=%s&id=%s`, in.Config.CaptchaSettings.ClientKey, t)
		req, err = http.NewRequest(http.MethodGet, resultEndpoint, nil)
		if err != nil {
			return "", fmt.Errorf("error creating request [%v]", err)
		}
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("error sending request [%v]", err)
		}
		defer resp.Body.Close()
		body, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			return "", fmt.Errorf("error reading response [%v]", err)
		}
		fmt.Println(string(body))
		var solution captchaAISolution
		err = json.Unmarshal(body, &solution)
		if err != nil {
			return "", fmt.Errorf("error unmarshalling response [%v]", err)
		}
		if solution.Status == "success" {
			return solution.Token, nil
		}
	}
	return "", fmt.Errorf("captcha timedout 120 seconds")
}

type captchaAISubmit struct {
	Type      string `json:"type"`
	SiteKey   string `json:"siteKey"`
	PageURL   string `json:"pageURL"`
	Proxy     string `json:"proxy,omitempty"`
	UserAgent string `json:"userAgent"`
	Timeout   int    `json:"timeout"`
	RqData    string `json:"rqdata,omitempty"`
}

type captchaAISubmitResponse struct {
	ID         string `json:"id"`
	Success    bool   `json:"success"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	ExpireTime int    `json:"expireTime"`
}

type captchaAISolution struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Status  string `json:"status"`
	Message string `json:"message"`
}
