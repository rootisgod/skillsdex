package skillsdex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type apiClient struct {
	base  string
	token string
	http  *http.Client
}

const cliRequestTimeout = 30 * time.Second

func authenticatedClient() (*apiClient, error) {
	cfg, err := loadAuth()
	if err != nil {
		return nil, err
	}
	return newAPIClient(cfg), nil
}

func newAPIClient(cfg AuthConfig) *apiClient {
	return &apiClient{
		base:  strings.TrimRight(cfg.ServerURL, "/"),
		token: cfg.Token,
		http:  &http.Client{Timeout: cliRequestTimeout},
	}
}

func (c *apiClient) newRequest(method, path string, body io.Reader) (*http.Request, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), cliRequestTimeout)
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		cancel()
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	return req, cancel, nil
}

func (c *apiClient) fetchSkill(owner, slug string) (skillResponse, error) {
	var resp skillResponse
	err := c.getJSON("/v1/skills/"+owner+"/"+slug, &resp)
	return resp, err
}

func (c *apiClient) fetchInstallSet(slug string) (installSetResponse, error) {
	var resp installSetResponse
	err := c.getJSON("/v1/install-sets/"+slug, &resp)
	return resp, err
}

func (c *apiClient) fetchPlugin(slug string) (pluginResponse, error) {
	var resp pluginResponse
	err := c.getJSON("/v1/plugins/"+slug, &resp)
	return resp, err
}

func (c *apiClient) refreshSkill(owner, slug string, dryRun bool) (refreshResponse, error) {
	var resp refreshResponse
	path := "/v1/skills/" + url.PathEscape(owner) + "/" + url.PathEscape(slug) + "/refresh"
	if dryRun {
		path += "?dry_run=1"
	}
	err := c.postJSON(path, map[string]bool{}, &resp)
	return resp, err
}

func (c *apiClient) updateSkillFlags(owner, slug string, flags SkillFlagUpdate) (Skill, error) {
	var resp struct {
		Skill Skill `json:"skill"`
	}
	err := c.putJSON("/v1/skills/"+url.PathEscape(owner)+"/"+url.PathEscape(slug)+"/flags", flags, &resp)
	return resp.Skill, err
}

func (c *apiClient) skillExists(owner, slug string) (bool, error) {
	path := "/v1/skills/" + url.PathEscape(owner) + "/" + url.PathEscape(slug)
	req, cancel, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	defer cancel()
	res, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return false, apiResponseError(http.MethodGet, path, res)
	}
	return true, nil
}

func (c *apiClient) getJSON(path string, out any) error {
	req, cancel, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	defer cancel()
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodGet, path, res)
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) getBytes(path string) ([]byte, error) {
	req, cancel, err := c.newRequest(http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	defer cancel()
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, apiResponseError(http.MethodGet, path, res)
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, maxSkillsDexExportBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSkillsDexExportBytes {
		return nil, errors.New("export download is too large")
	}
	return data, nil
}

func (c *apiClient) putJSON(path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, cancel, err := c.newRequest(http.MethodPut, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer cancel()
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodPut, path, res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) postJSON(path string, in, out any) error {
	body, _ := json.Marshal(in)
	req, cancel, err := c.newRequest(http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer cancel()
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodPost, path, res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *apiClient) postBytes(path string, data []byte, contentType string, out any) error {
	req, cancel, err := c.newRequest(http.MethodPost, path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer cancel()
	req.Header.Set("Content-Type", contentType)
	res, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return apiResponseError(http.MethodPost, path, res)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func apiResponseError(method, path string, res *http.Response) error {
	message := ""
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err == nil {
		if parsed := apiErrorMessage(body); parsed != "" {
			message = parsed
		} else if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			message = trimmed
		}
	}
	if message == "" {
		return fmt.Errorf("%s %s: %s", method, path, res.Status)
	}
	return fmt.Errorf("%s %s: %s: %s", method, path, res.Status, message)
}

func apiErrorMessage(body []byte) string {
	var payload struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.Error)
}
