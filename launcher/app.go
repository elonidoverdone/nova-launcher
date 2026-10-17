package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"nova/launcher/internal/dl"
	"nova/launcher/internal/lyrics"
	"nova/launcher/internal/mc"
	"nova/launcher/internal/media"
	"nova/launcher/internal/modrinth"
	"nova/launcher/internal/msauth"
	"nova/launcher/internal/secure"
	"nova/launcher/internal/settings"
)

const (
	appName    = "Nova Launcher"
	appVersion = "1.0.0"
	modGame    = "26.3" // NovaFX is built for this version
)

type Task struct {
	Busy    bool   `json:"busy"`
	Stage   string `json:"stage"`
	Done    int    `json:"done"`
	Total   int    `json:"total"`
	Error   string `json:"error"`
	Message string `json:"message"`
}

type LoginState struct {
	Active   bool   `json:"active"`
	UserCode string `json:"userCode"`
	URI      string `json:"uri"`
	Status   string `json:"status"`
	Error    string `json:"error"`
	// AccountID is set once a login finished (the new account is selected).
	AccountID string `json:"accountId,omitempty"`
}

type App struct {
	dataDir string
	exeDir  string
	store   *settings.Store
	env     mc.Env
	logger  *log.Logger

	// hooks provided by the window (no-ops in dev mode)
	Minimize func()
	Restore  func()
	OpenPath func(string) error

	mu          sync.Mutex
	task        Task
	login       LoginState
	loginCancel context.CancelFunc
	game        *exec.Cmd
	gameLog     []string
	lastExit    string
	packName    string

	lyrics       *lyrics.Service
	lyricsCancel context.CancelFunc
	modIndex     *modrinth.Index
	bridge       *bridgeState
	modIndexDir  string
}

func NewApp(dataDir, exeDir string) *App {
	_ = os.MkdirAll(filepath.Join(dataDir, "logs"), 0o755)
	lf, err := os.OpenFile(filepath.Join(dataDir, "logs", "launcher.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	var w io.Writer = os.Stderr
	if err == nil {
		w = lf
	}
	a := &App{
		dataDir:  dataDir,
		exeDir:   exeDir,
		store:    settings.Open(dataDir),
		env:      mc.HostEnv(),
		logger:   log.New(w, "", log.LstdFlags),
		Minimize: func() {},
		Restore:  func() {},
		OpenPath: func(string) error { return errors.New("not supported") },
	}
	a.lyrics = lyrics.NewService(&lyrics.Fetcher{CacheDir: filepath.Join(dataDir, "lyrics-cache")}, a.lyricsOutPath)
	if a.store.Get().Lyrics {
		a.startLyrics()
	}
	if err := a.startBridge(); err != nil {
		a.logger.Printf("bridge: %v", err)
	}
	return a
}

func (a *App) paths() mc.Paths {
	s := a.store.Get()
	return mc.Paths{Root: a.dataDir, GameDir: s.GameDir}
}

func (a *App) lyricsOutPath() string {
	return filepath.Join(a.paths().Config(), "novafx-lyrics.json")
}

func (a *App) index() *modrinth.Index {
	a.mu.Lock()
	defer a.mu.Unlock()
	mods := a.paths().Mods()
	if a.modIndex == nil || a.modIndexDir != mods {
		a.modIndex = modrinth.OpenIndex(mods)
		a.modIndexDir = mods
	}
	return a.modIndex
}

// ---------------------------------------------------------------- dispatcher

// Call runs a UI request. It may block (network), so callers run it off the UI thread.
func (a *App) Call(method string, raw json.RawMessage) (any, error) {
	var args map[string]any
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &args); err != nil {
			return nil, err
		}
	}
	str := func(k string) string { s, _ := args[k].(string); return strings.TrimSpace(s) }

	switch method {
	case "state":
		return a.State(), nil
	case "settings.save":
		return nil, a.saveSettings(args)
	case "account.offline":
		return nil, a.addOffline(str("name"))
	case "account.select":
		return nil, a.store.Update(func(s *settings.Settings) { s.Selected = str("id") })
	case "account.remove":
		return nil, a.store.Update(func(s *settings.Settings) { s.Remove(str("id")) })
	case "ms.start":
		return a.startMSLogin()
	case "ms.cancel":
		a.cancelMSLogin()
		return nil, nil
	case "game.launch":
		return nil, a.launch()
	case "game.stop":
		return nil, a.stopGame()
	case "versions":
		return a.versions()
	case "mods.search":
		return modrinth.Search(context.Background(), str("query"), a.store.Get().Version, a.index())
	case "mods.install":
		ix := a.index()
		return nil, modrinth.Install(context.Background(), str("id"), a.store.Get().Version, a.paths().Mods(), ix)
	case "mods.remove":
		return nil, modrinth.Remove(str("id"), a.paths().Mods(), a.index())
	case "mods.list":
		return modrinth.ListJars(a.paths().Mods()), nil
	case "open.folder":
		return nil, a.openFolder(str("which"))
	case "open.url":
		u := str("url")
		if !strings.HasPrefix(u, "https://") && !strings.HasPrefix(u, "http://") {
			return nil, errors.New("only http(s) links")
		}
		return nil, a.OpenPath(u)
	}
	return nil, fmt.Errorf("unknown method %s", method)
}

// ---------------------------------------------------------------- state

type accountView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type lyricsView struct {
	Status  string `json:"status"`
	Title   string `json:"title"`
	Artist  string `json:"artist"`
	App     string `json:"app"`
	Playing bool   `json:"playing"`
	Line    string `json:"line"`
	Next    string `json:"next"`
	Index   int    `json:"index"`
}

type State struct {
	App      string        `json:"app"`
	Version  string        `json:"appVersion"`
	Settings any           `json:"settings"`
	Accounts []accountView `json:"accounts"`
	Selected string        `json:"selected"`
	Task     Task          `json:"task"`
	Login    LoginState    `json:"login"`
	Running  bool          `json:"running"`
	LastExit string        `json:"lastExit"`
	GameLog  []string      `json:"gameLog"`
	Lyrics   lyricsView    `json:"lyrics"`
	NovaFX   string        `json:"novafx"` // path of the bundled mod jar, "" if missing
	Pack     string        `json:"pack"`   // installed modpack name, "" if none
	ModGame  string        `json:"modGame"`
	DataDir  string        `json:"dataDir"`
}

func (a *App) State() State {
	s := a.store.Get()
	st := State{App: appName, Version: appVersion, Selected: s.Selected, ModGame: modGame, DataDir: a.dataDir}
	st.Settings = map[string]any{
		"version": s.Version, "memoryMb": s.MemoryMB, "gameDir": s.GameDir, "javaPath": s.JavaPath,
		"jvmArgs": s.JVMArgs, "msClientId": s.ClientID, "lyrics": s.Lyrics, "minimizeOnRun": s.MinimizeOnRun,
		"fabulouslyOptimized": s.FabulouslyOptimized,
		"maxMemoryMb":         totalMemoryMB(),
	}
	for _, acc := range s.Accounts {
		st.Accounts = append(st.Accounts, accountView{ID: acc.ID, Name: acc.Name, Type: acc.Type})
	}
	if sel, ok := s.SelectedAccount(); ok {
		st.Selected = sel.ID
	}
	a.mu.Lock()
	st.Task = a.task
	st.Login = a.login
	st.Running = a.game != nil
	st.LastExit = a.lastExit
	st.Pack = a.packName
	if st.Pack == "" {
		if ps := modrinth.ReadPackState(s.GameDir); ps != nil {
			st.Pack = ps.Name
		}
	}
	n := len(a.gameLog)
	st.GameLog = append([]string(nil), a.gameLog[max(0, n-40):]...)
	a.mu.Unlock()

	out, pos := a.lyrics.Snapshot()
	lv := lyricsView{Status: out.Status, Title: out.Title, Artist: out.Artist, App: out.App, Playing: out.Playing}
	if out.Status == "ok" {
		i := sort.Search(len(out.Lines), func(i int) bool { return out.Lines[i].T > pos }) - 1
		lv.Index = i
		if i >= 0 {
			lv.Line = out.Lines[i].Text
		}
		if i+1 < len(out.Lines) {
			lv.Next = out.Lines[i+1].Text
		}
	}
	st.Lyrics = lv
	st.NovaFX = a.bundledMod()
	return st
}

// ---------------------------------------------------------------- settings

func (a *App) saveSettings(args map[string]any) error {
	err := a.store.Update(func(s *settings.Settings) {
		if v, ok := args["memoryMb"].(float64); ok && v >= 1024 {
			s.MemoryMB = int(v)
		}
		if v, ok := args["gameDir"].(string); ok && strings.TrimSpace(v) != "" {
			s.GameDir = filepath.Clean(strings.TrimSpace(v))
		}
		if v, ok := args["javaPath"].(string); ok {
			s.JavaPath = strings.TrimSpace(v)
		}
		if v, ok := args["jvmArgs"].(string); ok {
			s.JVMArgs = strings.TrimSpace(v)
		}
		if v, ok := args["msClientId"].(string); ok {
			s.ClientID = strings.TrimSpace(v)
		}
		if v, ok := args["version"].(string); ok && v != "" {
			s.Version = v
		}
		if v, ok := args["lyrics"].(bool); ok {
			s.Lyrics = v
		}
		if v, ok := args["minimizeOnRun"].(bool); ok {
			s.MinimizeOnRun = v
		}
		if v, ok := args["fabulouslyOptimized"].(bool); ok {
			s.FabulouslyOptimized = v
		}
	})
	if err != nil {
		return err
	}
	if a.store.Get().Lyrics {
		a.startLyrics()
	} else {
		a.stopLyrics()
	}
	if err := a.writeBridgeFile(); err != nil {
		a.logger.Printf("bridge: %v", err)
	}
	return nil
}

func (a *App) versions() ([]string, error) {
	m, err := mc.LoadManifest(context.Background(), a.paths())
	if err != nil {
		return []string{a.store.Get().Version}, nil
	}
	var out []string
	for _, v := range m.Versions {
		if v.Type == "release" {
			out = append(out, v.ID)
		}
		if len(out) >= 20 {
			break
		}
	}
	return out, nil
}

// ---------------------------------------------------------------- accounts

func (a *App) addOffline(name string) error {
	if !settings.ValidName(name) {
		return errors.New("ник: 3–16 символов, латиница, цифры и _")
	}
	return a.store.Update(func(s *settings.Settings) {
		s.Upsert(settings.Account{ID: settings.OfflineUUID(name), Name: name, Type: "offline"})
	})
}

func (a *App) startMSLogin() (any, error) {
	clientID := a.store.Get().ClientID
	ctx, cancel := context.WithTimeout(context.Background(), 16*time.Minute)
	dc, err := msauth.RequestDeviceCode(ctx, clientID)
	if err != nil {
		cancel()
		return nil, err
	}
	a.mu.Lock()
	if a.loginCancel != nil {
		a.loginCancel()
	}
	a.loginCancel = cancel
	a.login = LoginState{Active: true, UserCode: dc.UserCode, URI: dc.VerificationURI, Status: "Введите код на странице Microsoft"}
	a.mu.Unlock()
	_ = a.OpenPath(dc.VerificationURI)

	go func() {
		defer cancel()
		fail := func(err error) {
			a.mu.Lock()
			a.login = LoginState{Error: err.Error()}
			a.mu.Unlock()
		}
		tok, err := msauth.PollDeviceCode(ctx, clientID, dc)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				fail(err)
			}
			return
		}
		a.mu.Lock()
		a.login.Status = "Вход в Xbox и Minecraft…"
		a.mu.Unlock()
		sess, err := msauth.Login(ctx, tok)
		if err != nil {
			fail(err)
			return
		}
		if err := a.saveSession(sess); err != nil {
			fail(err)
			return
		}
		a.mu.Lock()
		a.login = LoginState{Status: "Готово: " + sess.Name, AccountID: sess.UUID}
		a.mu.Unlock()
	}()
	return map[string]string{"userCode": dc.UserCode, "uri": dc.VerificationURI}, nil
}

func (a *App) cancelMSLogin() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.loginCancel != nil {
		a.loginCancel()
		a.loginCancel = nil
	}
	a.login = LoginState{}
}

func (a *App) saveSession(sess *msauth.Session) error {
	refresh, err := secure.Encrypt(sess.RefreshToken)
	if err != nil {
		return err
	}
	access, err := secure.Encrypt(sess.AccessToken)
	if err != nil {
		return err
	}
	return a.store.Update(func(s *settings.Settings) {
		s.Upsert(settings.Account{
			ID: sess.UUID, Name: sess.Name, Type: "microsoft",
			RefreshEnc: refresh, AccessEnc: access, ExpiresAt: sess.ExpiresAt, XUID: sess.XUID,
		})
	})
}

// gameAccount returns launch credentials for the selected account. With no accounts at all the
// game starts offline as "Player" and the NovaFX account screen asks the player to sign in.
func (a *App) gameAccount(ctx context.Context) (mc.Account, error) {
	s := a.store.Get()
	acc, ok := s.SelectedAccount()
	if !ok {
		return mc.Account{Name: "Player", UUID: settings.OfflineUUID("Player"), UserType: "legacy"}, nil
	}
	return a.accountSession(ctx, acc)
}

// accountSession returns credentials for one account, refreshing Microsoft tokens when needed.
func (a *App) accountSession(ctx context.Context, acc settings.Account) (mc.Account, error) {
	if acc.Type != "microsoft" {
		return mc.Account{Name: acc.Name, UUID: acc.ID, UserType: "legacy"}, nil
	}
	if time.Until(acc.ExpiresAt) > 10*time.Minute {
		tok, err := secure.Decrypt(acc.AccessEnc)
		if err == nil && tok != "" {
			return mc.Account{Name: acc.Name, UUID: acc.ID, AccessToken: tok, XUID: acc.XUID, UserType: "msa"}, nil
		}
	}
	a.setTask(func(t *Task) {
		if t.Busy {
			t.Stage = "Обновление входа Microsoft"
		}
	})
	rt, err := secure.Decrypt(acc.RefreshEnc)
	if err != nil || rt == "" {
		return mc.Account{}, errors.New("войдите в Microsoft заново")
	}
	ms, err := msauth.Refresh(ctx, a.store.Get().ClientID, rt)
	if err != nil {
		return mc.Account{}, err
	}
	sess, err := msauth.Login(ctx, ms)
	if err != nil {
		return mc.Account{}, err
	}
	if err := a.saveSession(sess); err != nil {
		return mc.Account{}, err
	}
	return mc.Account{Name: sess.Name, UUID: sess.UUID, AccessToken: sess.AccessToken, XUID: sess.XUID, UserType: "msa"}, nil
}

// ---------------------------------------------------------------- launch

func (a *App) setTask(fn func(*Task)) {
	a.mu.Lock()
	fn(&a.task)
	a.mu.Unlock()
}

func (a *App) launch() error {
	a.mu.Lock()
	if a.task.Busy {
		a.mu.Unlock()
		return errors.New("уже идёт установка")
	}
	if a.game != nil {
		a.mu.Unlock()
		return errors.New("игра уже запущена")
	}
	a.task = Task{Busy: true, Stage: "Подготовка"}
	a.lastExit = ""
	a.mu.Unlock()

	go func() {
		err := a.doLaunch()
		a.setTask(func(t *Task) {
			t.Busy = false
			if err != nil {
				t.Error = err.Error()
				t.Stage = ""
				a.logger.Printf("launch failed: %v", err)
			} else {
				t.Error = ""
				t.Stage = ""
				t.Message = "Игра запущена"
			}
		})
	}()
	return nil
}

func (a *App) doLaunch() error {
	ctx := context.Background()
	s := a.store.Get()
	p := a.paths()
	for _, d := range []string{p.GameDir, p.Mods(), p.Config()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	if err := a.writeBridgeFile(); err != nil {
		a.logger.Printf("bridge: %v", err)
	}
	account, err := a.gameAccount(ctx)
	if err != nil {
		return err
	}

	progress := func(stage string, done, total int) {
		a.setTask(func(t *Task) { t.Stage, t.Done, t.Total = stage, done, total })
	}
	// Fabulously Optimized first: it decides the Fabric Loader version and brings Fabric API
	loader := ""
	ix := a.index()
	if s.FabulouslyOptimized {
		pack, err := modrinth.InstallPack(ctx, modrinth.FabulouslyOptimized, s.Version, p.GameDir,
			filepath.Join(a.dataDir, "packs"), progress)
		if err != nil {
			return fmt.Errorf("Fabulously Optimized: %w", err)
		}
		loader = pack.Loader
		a.mu.Lock()
		a.packName = pack.Name
		a.mu.Unlock()
		// the pack ships its own Fabric API: drop ours so there are no duplicates
		for _, f := range pack.Files {
			if strings.HasPrefix(strings.ToLower(filepath.Base(f)), "fabric-api") {
				_ = modrinth.Remove(modrinth.FabricAPIProject, p.Mods(), ix)
				break
			}
		}
	} else if modrinth.ReadPackState(p.GameDir) != nil {
		if err := modrinth.RemovePack(p.GameDir); err != nil {
			a.logger.Printf("remove pack: %v", err)
		}
	}

	inst, err := mc.Install(ctx, p, s.Version, loader, a.env, progress)
	if err != nil {
		return err
	}

	// Fabric API (most mods, including NovaFX, need it)
	if !hasJar(p.Mods(), "fabric-api") {
		progress("Fabric API", 0, 0)
		if err := modrinth.Install(ctx, modrinth.FabricAPIProject, s.Version, p.Mods(), ix); err != nil {
			return fmt.Errorf("не удалось поставить Fabric API: %w", err)
		}
	}
	if s.Version == modGame {
		if err := a.installBundledMod(p.Mods()); err != nil {
			a.logger.Printf("novafx: %v", err)
		}
	}

	java := inst.JavaPath
	if s.JavaPath != "" {
		java = s.JavaPath
	}
	args, err := mc.BuildArgs(mc.LaunchOptions{
		Paths: p, Installed: inst, MemoryMB: s.MemoryMB, ExtraJVM: strings.Fields(s.JVMArgs),
		Account: account, Env: a.env, Launcher: "nova-launcher", LVersion: appVersion,
	})
	if err != nil {
		return err
	}
	return a.startGame(java, args, p.GameDir)
}

func (a *App) startGame(java string, args []string, dir string) error {
	cmd := exec.Command(java, args...)
	cmd.Dir = dir
	hideConsole(cmd)
	logPath := filepath.Join(a.dataDir, "logs", "game.log")
	lf, err := os.Create(logPath)
	if err != nil {
		return err
	}
	pr, pw := io.Pipe()
	cmd.Stdout = pw
	cmd.Stderr = pw
	if err := cmd.Start(); err != nil {
		lf.Close()
		return fmt.Errorf("не удалось запустить Java: %w", err)
	}
	a.mu.Lock()
	a.game = cmd
	a.gameLog = nil
	a.mu.Unlock()
	if a.store.Get().MinimizeOnRun {
		a.Minimize()
	}

	go func() {
		buf := make([]byte, 32*1024)
		var pending string
		for {
			n, err := pr.Read(buf)
			if n > 0 {
				lf.Write(buf[:n])
				pending += string(buf[:n])
				lines := strings.Split(pending, "\n")
				pending = lines[len(lines)-1]
				a.mu.Lock()
				for _, l := range lines[:len(lines)-1] {
					a.gameLog = append(a.gameLog, strings.TrimRight(l, "\r"))
				}
				if len(a.gameLog) > 400 {
					a.gameLog = a.gameLog[len(a.gameLog)-400:]
				}
				a.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		err := cmd.Wait()
		pw.Close()
		lf.Close()
		a.mu.Lock()
		a.game = nil
		if err != nil {
			a.lastExit = "Игра закрылась с ошибкой: " + err.Error() + " (лог: " + logPath + ")"
		} else {
			a.lastExit = "Игра закрыта"
		}
		a.mu.Unlock()
		a.Restore()
	}()
	return nil
}

func (a *App) stopGame() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.game == nil || a.game.Process == nil {
		return nil
	}
	return a.game.Process.Kill()
}

func hasJar(dir, prefix string) bool {
	for _, j := range modrinth.ListJars(dir) {
		if strings.HasPrefix(strings.ToLower(j), prefix) {
			return true
		}
	}
	return false
}

// bundledMod finds novafx-*.jar next to the launcher (or in a "mods-bundle" folder there).
func (a *App) bundledMod() string {
	for _, dir := range []string{a.exeDir, filepath.Join(a.exeDir, "mods-bundle")} {
		matches, _ := filepath.Glob(filepath.Join(dir, "novafx-*.jar"))
		sort.Strings(matches)
		for i := len(matches) - 1; i >= 0; i-- {
			if !strings.HasSuffix(matches[i], "-sources.jar") {
				return matches[i]
			}
		}
	}
	return ""
}

func (a *App) installBundledMod(mods string) error {
	src := a.bundledMod()
	if src == "" {
		return errors.New("novafx jar not found next to the launcher")
	}
	name := filepath.Base(src)
	dst := filepath.Join(mods, name)
	if sa, err := dl.FileSHA1(src); err == nil {
		if sb, err := dl.FileSHA1(dst); err == nil && sa == sb {
			return nil
		}
	}
	// remove other NovaFX versions
	old, _ := filepath.Glob(filepath.Join(mods, "novafx-*.jar"))
	for _, o := range old {
		if filepath.Base(o) != name {
			_ = os.Remove(o)
		}
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0o644)
}

// ---------------------------------------------------------------- lyrics

func (a *App) startLyrics() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.lyricsCancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.lyricsCancel = cancel
	go media.Watch(ctx, func(s lyrics.Sample) { a.lyrics.Feed(ctx, s) }, a.logger.Printf)
}

func (a *App) stopLyrics() {
	a.mu.Lock()
	cancel := a.lyricsCancel
	a.lyricsCancel = nil
	a.mu.Unlock()
	if cancel != nil {
		cancel()
		a.lyrics.Clear()
	}
}

// ---------------------------------------------------------------- misc

func (a *App) openFolder(which string) error {
	p := a.paths()
	dir := map[string]string{
		"game": p.GameDir, "mods": p.Mods(), "config": p.Config(),
		"logs": filepath.Join(a.dataDir, "logs"), "screenshots": filepath.Join(p.GameDir, "screenshots"),
		"launcher": a.exeDir,
	}[which]
	if dir == "" {
		return errors.New("unknown folder")
	}
	_ = os.MkdirAll(dir, 0o755)
	return a.OpenPath(dir)
}

func (a *App) Shutdown() {
	a.stopBridge()
	a.stopLyrics()
	a.cancelMSLogin()
}
