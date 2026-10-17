// Package msauth signs a Microsoft account into Minecraft using the OAuth device code flow:
// Microsoft -> Xbox Live -> XSTS -> Minecraft services.
//
// It needs an Azure application (client ID) that Mojang has approved for the Minecraft API.
// See README for how to register one.
package msauth

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

	"nova/launcher/internal/dl"
)

var (
	DeviceCodeURL = "https://login.microsoftonline.com/consumers/oauth2/v2.0/devicecode"
	TokenURL      = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	XBLURL        = "https://user.auth.xboxlive.com/user/authenticate"
	XSTSURL       = "https://xsts.auth.xboxlive.com/xsts/authorize"
	MCLoginURL    = "https://api.minecraftservices.com/authentication/login_with_xbox"
	MCProfileURL  = "https://api.minecraftservices.com/minecraft/profile"
)

const scope = "XboxLive.signin offline_access"

var client = &http.Client{Timeout: 30 * time.Second}

type DeviceCode struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	Message         string `json:"message"`
}

type MSTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Error        string `json:"error"`
	ErrorDesc    string `json:"error_description"`
}

// Session is what the game needs.
type Session struct {
	Name         string
	UUID         string
	AccessToken  string // Minecraft access token
	ExpiresAt    time.Time
	XUID         string
	RefreshToken string // Microsoft refresh token, store encrypted
}

func postForm(ctx context.Context, u string, form url.Values, out any) (int, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", dl.UserAgent)
	return do(req, out)
}

func postJSON(ctx context.Context, u string, body any, out any) (int, error) {
	b, _ := json.Marshal(body)
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", dl.UserAgent)
	return do(req, out)
}

func do(req *http.Request, out any) (int, error) {
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, err
	}
	if len(data) > 0 && out != nil {
		if err := json.Unmarshal(data, out); err != nil && resp.StatusCode < 300 {
			return resp.StatusCode, fmt.Errorf("bad response from %s", req.URL.Host)
		}
	}
	return resp.StatusCode, nil
}

func RequestDeviceCode(ctx context.Context, clientID string) (*DeviceCode, error) {
	if clientID == "" {
		return nil, errors.New("не задан Client ID приложения Azure (Настройки → Вход Microsoft)")
	}
	var resp struct {
		DeviceCode
		Error     string `json:"error"`
		ErrorDesc string `json:"error_description"`
	}
	status, err := postForm(ctx, DeviceCodeURL, url.Values{"client_id": {clientID}, "scope": {scope}}, &resp)
	if err != nil {
		return nil, err
	}
	dc := resp.DeviceCode
	if status != 200 || dc.DeviceCode == "" {
		return nil, fmt.Errorf("Microsoft отклонил запрос кода (%d): %s", status, resp.ErrorDesc)
	}
	if dc.Interval <= 0 {
		dc.Interval = 5
	}
	return &dc, nil
}

// PollDeviceCode waits until the user enters the code (or it expires).
func PollDeviceCode(ctx context.Context, clientID string, dc *DeviceCode) (*MSTokens, error) {
	interval := time.Duration(dc.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(dc.ExpiresIn) * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(interval):
		}
		var t MSTokens
		_, err := postForm(ctx, TokenURL, url.Values{
			"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
			"client_id":   {clientID},
			"device_code": {dc.DeviceCode},
		}, &t)
		if err != nil {
			continue // transient network error, keep polling
		}
		switch t.Error {
		case "":
			if t.AccessToken != "" {
				return &t, nil
			}
		case "authorization_pending":
		case "slow_down":
			interval += 5 * time.Second
		case "authorization_declined":
			return nil, errors.New("вход отклонён")
		case "expired_token":
			return nil, errors.New("код истёк, начните вход заново")
		default:
			return nil, fmt.Errorf("ошибка входа: %s", t.ErrorDesc)
		}
	}
	return nil, errors.New("код истёк, начните вход заново")
}

func Refresh(ctx context.Context, clientID, refreshToken string) (*MSTokens, error) {
	var t MSTokens
	_, err := postForm(ctx, TokenURL, url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"scope":         {scope},
	}, &t)
	if err != nil {
		return nil, err
	}
	if t.AccessToken == "" {
		return nil, fmt.Errorf("не удалось обновить вход: %s", t.ErrorDesc)
	}
	if t.RefreshToken == "" {
		t.RefreshToken = refreshToken
	}
	return &t, nil
}

type xboxResp struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		Xui []struct {
			Uhs string `json:"uhs"`
			Xid string `json:"xid"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
	XErr    int64  `json:"XErr"`
	Message string `json:"Message"`
}

// Login turns a Microsoft access token into a Minecraft session.
func Login(ctx context.Context, ms *MSTokens) (*Session, error) {
	var xbl xboxResp
	if _, err := postJSON(ctx, XBLURL, map[string]any{
		"Properties": map[string]any{
			"AuthMethod": "RPS",
			"SiteName":   "user.auth.xboxlive.com",
			"RpsTicket":  "d=" + ms.AccessToken,
		},
		"RelyingParty": "http://auth.xboxlive.com",
		"TokenType":    "JWT",
	}, &xbl); err != nil {
		return nil, err
	}
	if xbl.Token == "" || len(xbl.DisplayClaims.Xui) == 0 {
		return nil, errors.New("Xbox Live не выдал токен")
	}

	var xsts xboxResp
	if _, err := postJSON(ctx, XSTSURL, map[string]any{
		"Properties": map[string]any{
			"SandboxId":  "RETAIL",
			"UserTokens": []string{xbl.Token},
		},
		"RelyingParty": "rp://api.minecraftservices.com/",
		"TokenType":    "JWT",
	}, &xsts); err != nil {
		return nil, err
	}
	switch xsts.XErr {
	case 0:
	case 2148916233:
		return nil, errors.New("у этого аккаунта Microsoft нет профиля Xbox — создайте его на xbox.com")
	case 2148916235:
		return nil, errors.New("Xbox Live недоступен в вашей стране")
	case 2148916236, 2148916237:
		return nil, errors.New("аккаунту нужна верификация возраста на xbox.com")
	case 2148916238:
		return nil, errors.New("детский аккаунт: добавьте его в семейную группу Microsoft")
	default:
		return nil, fmt.Errorf("XSTS ошибка %d", xsts.XErr)
	}
	uhs := xbl.DisplayClaims.Xui[0].Uhs

	var mc struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
		ErrorMsg    string `json:"errorMessage"`
	}
	status, err := postJSON(ctx, MCLoginURL, map[string]string{"identityToken": "XBL3.0 x=" + uhs + ";" + xsts.Token}, &mc)
	if err != nil {
		return nil, err
	}
	if mc.AccessToken == "" {
		if status == 403 {
			return nil, errors.New("Minecraft API отклонил приложение (403): Client ID ещё не одобрен Mojang — см. README")
		}
		return nil, fmt.Errorf("вход в Minecraft не удался (%d) %s", status, mc.ErrorMsg)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, MCProfileURL, nil)
	req.Header.Set("Authorization", "Bearer "+mc.AccessToken)
	var prof struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	status, err = do(req, &prof)
	if err != nil {
		return nil, err
	}
	if status == 404 || prof.ID == "" {
		return nil, errors.New("на этом аккаунте не куплен Minecraft: Java Edition")
	}
	xid := ""
	if len(xsts.DisplayClaims.Xui) > 0 {
		xid = xsts.DisplayClaims.Xui[0].Xid
	}
	return &Session{
		Name:         prof.Name,
		UUID:         prof.ID,
		AccessToken:  mc.AccessToken,
		ExpiresAt:    time.Now().Add(time.Duration(mc.ExpiresIn) * time.Second),
		XUID:         xid,
		RefreshToken: ms.RefreshToken,
	}, nil
}
