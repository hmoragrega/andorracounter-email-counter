package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type DaysApi struct {
	URL      string
	Username string
	Password string
	client   *http.Client
}

func NewDaysApi(url, username, password string) *DaysApi {
	return &DaysApi{
		URL:      url,
		Username: username,
		Password: password,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

type Day struct {
	Day     string `json:"day"`
	Andorra int    `json:"andorra"`
	Spain   int    `json:"spain"`
	World   int    `json:"world"`
	Note    string `json:"note,omitempty"`
}

func (api *DaysApi) GetDay(ctx context.Context, day string) (res *Day, err error) {
	// add basic auth to request
	req, err := http.NewRequestWithContext(ctx, "GET", api.URL+"/days/"+day, nil)
	if err != nil {
		return res, fmt.Errorf("creating request: %v", err)
	}
	req.SetBasicAuth(api.Username, api.Password)
	resp, err := api.client.Do(req)
	if err != nil {
		return res, fmt.Errorf("making request: %v", err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return res, fmt.Errorf("reading response body (code: %d): %v", resp.StatusCode, err)
	}
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return res, fmt.Errorf("unexpected status code: %d (body %s)", resp.StatusCode, string(b))
	}
	res = new(Day)
	err = json.Unmarshal(b, &res)
	if err != nil {
		return res, fmt.Errorf("unmarshaling response body (code: %d): %v", resp.StatusCode, err)
	}
	return res, nil
}

func (api *DaysApi) UpdateDay(ctx context.Context, day Day) error {
	b, err := json.Marshal(day)
	if err != nil {
		return fmt.Errorf("marshaling update day: %v", err)
	}
	body := bytes.NewBuffer(b)
	req, err := http.NewRequestWithContext(ctx, "POST", api.URL+"/days/"+day.Day, body)
	if err != nil {
		return fmt.Errorf("creating update day  request: %v", err)
	}
	req.SetBasicAuth(api.Username, api.Password)
	req.Header.Set("Content-Type", "application/json")
	resp, err := api.client.Do(req)
	if err != nil {
		return fmt.Errorf("making update day request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("reading update day response body (code: %d): %v", resp.StatusCode, err)
		}
		return fmt.Errorf("unexpected status code: %d (body %s)", resp.StatusCode, string(b))
	}
	return nil
}
