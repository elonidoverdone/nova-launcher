package msauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeviceFlowAndLogin(t *testing.T) {
	polls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		switch r.URL.Path {
		case "/devicecode":
			if r.Form.Get("client_id") != "cid" || r.Form.Get("scope") != "XboxLive.signin offline_access" {
				t.Errorf("bad device code form %v", r.Form)
			}
			json.NewEncoder(w).Encode(map[string]any{"device_code": "dc", "user_code": "ABCD-1234", "verification_uri": "https://microsoft.com/link", "expires_in": 60, "interval": 0})
		case "/token":
			polls++
			if polls < 2 {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]any{"error": "authorization_pending"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "ms-at", "refresh_token": "ms-rt", "expires_in": 3600})
		case "/xbl":
			var body map[string]map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["Properties"]["RpsTicket"] != "d=ms-at" {
				t.Errorf("bad RpsTicket %v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{"Token": "xbl", "DisplayClaims": map[string]any{"xui": []any{map[string]string{"uhs": "U"}}}})
		case "/xsts":
			json.NewEncoder(w).Encode(map[string]any{"Token": "xsts", "DisplayClaims": map[string]any{"xui": []any{map[string]string{"uhs": "U", "xid": "42"}}}})
		case "/mc":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["identityToken"] != "XBL3.0 x=U;xsts" {
				t.Errorf("bad identity token %v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "mc-at", "expires_in": 86400})
		case "/profile":
			if r.Header.Get("Authorization") != "Bearer mc-at" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"id": "uuid1", "name": "Steve"})
		}
	}))
	defer srv.Close()
	DeviceCodeURL, TokenURL, XBLURL, XSTSURL, MCLoginURL, MCProfileURL =
		srv.URL+"/devicecode", srv.URL+"/token", srv.URL+"/xbl", srv.URL+"/xsts", srv.URL+"/mc", srv.URL+"/profile"

	ctx := context.Background()
	if _, err := RequestDeviceCode(ctx, ""); err == nil {
		t.Error("empty client id must fail")
	}
	dc, err := RequestDeviceCode(ctx, "cid")
	if err != nil || dc.UserCode != "ABCD-1234" {
		t.Fatalf("device code: %v %+v", err, dc)
	}
	dc.Interval = 0
	tok, err := PollDeviceCode(ctx, "cid", dc)
	if err != nil || tok.RefreshToken != "ms-rt" || polls != 2 {
		t.Fatalf("poll: %v %+v polls=%d", err, tok, polls)
	}
	s, err := Login(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "Steve" || s.UUID != "uuid1" || s.AccessToken != "mc-at" || s.XUID != "42" || s.RefreshToken != "ms-rt" {
		t.Errorf("session %+v", s)
	}
}

func TestXSTSErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "xbl") {
			json.NewEncoder(w).Encode(map[string]any{"Token": "x", "DisplayClaims": map[string]any{"xui": []any{map[string]string{"uhs": "U"}}}})
			return
		}
		w.WriteHeader(401)
		json.NewEncoder(w).Encode(map[string]any{"XErr": 2148916233})
	}))
	defer srv.Close()
	XBLURL, XSTSURL = srv.URL+"/xbl", srv.URL+"/xsts"
	_, err := Login(context.Background(), &MSTokens{AccessToken: "a"})
	if err == nil || !strings.Contains(err.Error(), "Xbox") {
		t.Errorf("expected Xbox profile error, got %v", err)
	}
}
