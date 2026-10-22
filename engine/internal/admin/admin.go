package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"desk/internal/model"
)

const (
	defaultSupabase = "https://papzztaomhzhwtcnvhhf.supabase.co"
	proxySupabase   = "https://sb-proxy.dznovel.top"
	// Public catalogue key used by the administration site. It cannot read provider secrets.
	anonKey = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJpc3MiOiJzdXBhYmFzZSIsInJlZiI6InBhcHp6dGFvbWh6aHd0Y252aGhmIiwicm9sZSI6ImFub24iLCJpYXQiOjE3NTA3MzU3MTMsImV4cCI6MjA2NjMxMTcxM30.G3vOSyflF5Cb2NBRYTGUnVdrHAcgGYqL4AdmYwNaGoM"
)

type Resolved struct {
	ProviderID string
	APIKey     string
	APIURL     string
	Model      string
}

type officialModel struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Provider          string `json:"provider"`
	ProviderID        string `json:"provider_id"`
	PreferredProvider string `json:"preferred_provider_id"`
	Upstream          string `json:"upstream_model_name"`
	ModelType         string `json:"model_type"`
	Active            *bool  `json:"is_active"`
	Sort              int    `json:"sort_order"`
}

type providerRow struct {
	ID       string `json:"id"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	ChatPath string `json:"chat_path"`
	Format   string `json:"api_format"`
	Enabled  bool   `json:"enabled"`
}

func List(ctx context.Context) ([]model.AdminModel, error) {
	query := url.Values{
		"select":    {"id,name,provider,provider_id,preferred_provider_id,upstream_model_name,model_type,is_active,sort_order"},
		"is_active": {"eq.true"},
		"order":     {"sort_order.asc"},
		"limit":     {"500"},
	}
	var rows []officialModel
	if err := getJSON(ctx, catalogueBases(), anonKey, "official_models", query, &rows); err != nil {
		return nil, err
	}
	out := make([]model.AdminModel, 0, len(rows))
	for _, row := range rows {
		if row.ModelType != "" && row.ModelType != "text" {
			continue
		}
		if row.Active != nil && !*row.Active {
			continue
		}
		name := strings.TrimSpace(row.Name)
		id := strings.TrimSpace(row.ID)
		upstream := strings.TrimSpace(row.Upstream)
		if upstream == "" {
			upstream = id
		}
		providerID := first(row.PreferredProvider, row.ProviderID, row.Provider)
		if id == "" || name == "" || upstream == "" || providerID == "" {
			continue
		}
		out = append(out, model.AdminModel{ID: id, Name: name, ProviderID: providerID, UpstreamModel: upstream})
	}
	if len(out) == 0 {
		return nil, errors.New("后台没有可用的文本模型")
	}
	return out, nil
}

func Resolve(ctx context.Context, providerID, upstream string) (Resolved, error) {
	providerID, upstream = strings.TrimSpace(providerID), strings.TrimSpace(upstream)
	if providerID == "" || upstream == "" {
		return Resolved{}, errors.New("先在设置里选择模型")
	}
	token := first(os.Getenv("DESK_ADMIN_SUPABASE_SECRET_KEY"), os.Getenv("NOFX_ADMIN_SUPABASE_SECRET_KEY"), os.Getenv("NOFX_ADMIN_SUPABASE_SERVICE_ROLE_KEY"))
	if token == "" {
		return Resolved{}, errors.New("引擎没有后台密钥，无法替你调用模型")
	}
	var rows []providerRow
	query := url.Values{
		"select": {"id,base_url,api_key,chat_path,api_format,enabled"},
		"id":     {"eq." + providerID},
		"limit":  {"1"},
	}
	if err := getJSON(ctx, secretBases(), token, "providers", query, &rows); err != nil {
		return Resolved{}, fmt.Errorf("读取模型通道失败：%w", err)
	}
	if len(rows) != 1 || !rows[0].Enabled || strings.TrimSpace(rows[0].APIKey) == "" {
		return Resolved{}, errors.New("这个模型通道已停用，或后台没有配密钥")
	}
	row := rows[0]
	if row.Format != "" && !strings.Contains(strings.ToLower(row.Format), "openai") {
		return Resolved{}, fmt.Errorf("模型通道 %s 不是通用对话接口", providerID)
	}
	apiURL := strings.TrimRight(strings.TrimSpace(row.BaseURL), "/")
	chatPath := strings.TrimSpace(row.ChatPath)
	if chatPath == "" {
		chatPath = "/chat/completions"
	}
	if strings.HasPrefix(chatPath, "https://") {
		apiURL = chatPath
	} else if !strings.HasSuffix(apiURL, "/"+strings.TrimLeft(chatPath, "/")) {
		apiURL += "/" + strings.TrimLeft(chatPath, "/")
	}
	parsed, err := url.Parse(apiURL)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return Resolved{}, errors.New("模型通道地址无效")
	}
	return Resolved{ProviderID: providerID, APIKey: strings.TrimSpace(row.APIKey), APIURL: apiURL, Model: upstream}, nil
}

func catalogueBases() []string {
	return unique(proxySupabase, defaultSupabase, strings.TrimSpace(os.Getenv("DESK_ADMIN_SUPABASE_URL")))
}

func secretBases() []string {
	return unique(strings.TrimSpace(os.Getenv("DESK_ADMIN_SUPABASE_URL")), strings.TrimSpace(os.Getenv("NOFX_ADMIN_SUPABASE_URL")), defaultSupabase, proxySupabase)
}

func getJSON(ctx context.Context, bases []string, token, table string, query url.Values, target any) error {
	var last error
	for _, base := range bases {
		err := getOne(ctx, base, token, table, query, target)
		if err == nil {
			return nil
		}
		last = err
	}
	if last == nil {
		last = errors.New("没有可用的后台地址")
	}
	return last
}

func getOne(ctx context.Context, base, token, table string, query url.Values, target any) error {
	parsed, err := url.Parse(base)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return errors.New("后台地址无效")
	}
	endpoint := strings.TrimRight(base, "/") + "/rest/v1/" + table
	if query != nil {
		endpoint += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("apikey", token)
	req.Header.Set("Accept", "application/json")
	if !strings.HasPrefix(token, "sb_secret_") {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("后台跳转被拒绝")
	}}
	res, err := client.Do(req)
	if err != nil {
		return errors.New("后台连不上")
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("后台返回 HTTP %d", res.StatusCode)
	}
	if err := json.Unmarshal(body, target); err != nil {
		return errors.New("后台返回的内容无法解析")
	}
	return nil
}

func first(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func unique(values ...string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, v := range values {
		v = strings.TrimRight(strings.TrimSpace(v), "/")
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
