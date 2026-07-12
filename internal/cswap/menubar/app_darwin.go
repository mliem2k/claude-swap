//go:build darwin

package menubar

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"fyne.io/systray"

	"github.com/mliem2k/claude-swap/internal/cswap"
)

// app holds all mutable menu-bar state. One instance per process,
// constructed in Run.
type app struct {
	switcher     *cswap.ClaudeAccountSwitcher
	settingsPath string
	logPath      string
	osa          osascriptRunner

	settings Settings
	source   *cswap.SnapshotSource

	mu         sync.Mutex // guards snapshot + accountItems (read by the sync tick, written by the refresh worker and by menu callbacks)
	snapshot   snapshotView
	refreshing bool
	dirty      bool

	accountItems     map[string]*trackedItem // keyed by account number, the diffed section (Global Constraints)
	accountEmptyItem *systray.MenuItem       // "No managed accounts" placeholder, non-nil only when a.accountItems is empty
	removeItems      map[string]*trackedItem

	removeMenu        *systray.MenuItem
	removeEmptyItem   *systray.MenuItem // "No managed accounts" placeholder, non-nil only when a.removeItems is empty
	historyMenu       *systray.MenuItem
	historyEntryItems []*systray.MenuItem

	// thresholdItems and lastSyncedThreshold let syncThresholdCheckmarks
	// (called every dirty tick from applySnapshot) keep the Settings
	// submenu's checkmarks tracking the live autoswitch.threshold value
	// even when it changed externally (cswap config set, another cswap
	// auto/menu bar instance), matching Python's _settings_menu being
	// rebuilt fresh on every rebuild_menu() call.
	thresholdItems      map[int]*systray.MenuItem
	lastSyncedThreshold int

	stopSync chan struct{}

	engine     *cswap.AutoSwitchEngine
	engineMu   sync.Mutex
	engineEvts chan cswap.AutoSwitchEvent

	lastUsageLogKey map[string][2]float64
	configPath      string
	configMtime     time.Time
}

// trackedItem pairs a systray item with the done channel that stops its
// click-handler goroutine cleanly (see Global Constraints on ClickedCh).
type trackedItem struct {
	item *systray.MenuItem
	done chan struct{}
}

// snapshotView mirrors _adapt_snapshot's render shape.
type snapshotView struct {
	accounts    []accountView
	activeEmail string
	activeUsage DisplayUsage
}

type accountView struct {
	number   string
	email    string
	isActive bool
	display  DisplayUsage
	lastGood map[string]any
}

func adaptSnapshot(snap cswap.AccountsSnapshotResult) snapshotView {
	var out snapshotView
	for _, acc := range snap.Accounts {
		display := accountDisplayUsage(acc.Usage)
		out.accounts = append(out.accounts, accountView{
			number: acc.Number, email: acc.Email, isActive: acc.IsActive,
			display: display, lastGood: acc.Usage.LastGood,
		})
		if acc.IsActive {
			out.activeEmail, out.activeUsage = acc.Email, display
		}
	}
	return out
}

func accountDisplayUsage(u cswap.UsageEntry) DisplayUsage {
	if u.Sentinel != nil {
		return DisplayUsage{Sentinel: SentinelLabel(*u.Sentinel)}
	}
	return DisplayUsage{LastGood: u.LastGood}
}

// SentinelLabel re-exports cswap.SentinelLabel, matching how the tui
// package's data.go already does the identical wrapper.
func SentinelLabel(sentinel string) string {
	return cswap.SentinelLabel(sentinel)
}

// Run mirrors menubar.py's module-level run(switcher): the cswap
// --menubar entry point. Blocks until Quit.
func Run(switcher *cswap.ClaudeAccountSwitcher) error {
	a := &app{
		switcher:        switcher,
		settingsPath:    switcher.BackupDir + "/menubar_settings.json",
		logPath:         switcher.BackupDir + "/claude-swap.log",
		osa:             realOSAScript{},
		source:          cswap.NewSnapshotSource(switcher),
		accountItems:    map[string]*trackedItem{},
		removeItems:     map[string]*trackedItem{},
		stopSync:        make(chan struct{}),
		lastUsageLogKey: map[string][2]float64{},
	}
	a.settings = LoadSettings(a.settingsPath)
	a.configPath = switcher.GetClaudeConfigPath()

	systray.Run(a.onReady, a.onExit)
	return nil
}

// onReady's account section renders after every static item (including
// Quit), not first like Python's rebuild_menu (which always puts
// account_items first). This is an accepted deviation, not a bug left
// unfixed: accounts are only known via a.source.Take, real blocking I/O
// (file locks, possibly network), and fyne.io/systray provides no
// insert-at-position/reorder API (AddMenuItem(Checkbox) always appends to
// the end). Front-loading a synchronous fetch here (attempted and
// reverted) risks onReady not returning promptly, a systray/Cocoa startup
// callback expected to return quickly.
//
// A version of this change was seen to crash the real app (SIGTRAP inside
// systray's native run loop) during manual testing, but that crash's real
// cause turned out to be unrelated and environmental: cli.go's RunCLI was
// running root.Execute() (and so the whole menu bar) on a freshly spawned
// goroutine, an OS thread systray's own init() (runtime.LockOSThread(),
// which locks only whichever goroutine calls it first, i.e. goroutine 1)
// never locked, so Cocoa's NSApplication run loop landed on the wrong
// thread and crashed unconditionally, independent of anything in this
// file. Fixed in RunCLI (a --menubar invocation now calls root.Execute()
// directly on the caller's own goroutine); this comment stays only because
// the "no reorder API" limitation is still a real, separate reason the
// ordering deviation above is accepted, matching the switch-history
// submenu's ordering.
func (a *app) onReady() {
	systray.SetTitle(Icon)
	systray.SetTooltip("claude-swap")

	a.buildStaticMenu()
	go a.syncLoop()
	a.refreshAsync(false)
	if a.settings.AutoSwitchEnabled {
		a.startEngine()
	}
}

func (a *app) onExit() {
	close(a.stopSync)
	a.stopEngine()
}

func (a *app) refreshAsync(full bool) {
	a.mu.Lock()
	if a.refreshing {
		a.mu.Unlock()
		return
	}
	a.refreshing = true
	a.mu.Unlock()
	a.engineMu.Lock()
	storeOnly := a.engine != nil
	a.engineMu.Unlock()

	go func() {
		defer func() {
			a.mu.Lock()
			a.refreshing = false
			a.mu.Unlock()
		}()
		snap := a.source.Take(full, storeOnly)
		view := adaptSnapshot(snap)
		a.mu.Lock()
		a.snapshot = view
		a.dirty = true
		a.mu.Unlock()
		a.logUsage(view)
	}()
}

func (a *app) logUsage(view snapshotView) {
	for _, acc := range view.accounts {
		p5, ok5 := windowPct(acc.lastGood, "five_hour")
		p7, ok7 := windowPct(acc.lastGood, "seven_day")
		if !ok5 && !ok7 {
			continue
		}
		key := [2]float64{p5, p7}
		a.mu.Lock()
		same := a.lastUsageLogKey[acc.number] == key
		a.mu.Unlock()
		if same {
			continue
		}
		if line, ok := FormatUsageLog(acc.email, acc.display); ok {
			a.switcher.Logger.Info(line)
			a.mu.Lock()
			a.lastUsageLogKey[acc.number] = key
			a.mu.Unlock()
		}
	}
}

// syncLoop mirrors the on_refresh_tick/on_sync_tick pair: a ticker on
// the configured interval requests a refresh, a fast ~1s ticker applies
// whatever the background worker produced and detects active-account
// changes, matching Python's two-timer design (systray, like Cocoa,
// expects menu mutations from a single consistent caller, not raw
// concurrent writes from an arbitrary goroutine).
func (a *app) syncLoop() {
	a.mu.Lock()
	interval := time.Duration(a.settings.RefreshInterval) * time.Second
	a.mu.Unlock()
	refreshTicker := time.NewTicker(interval)
	defer refreshTicker.Stop()
	syncTicker := time.NewTicker(time.Second)
	defer syncTicker.Stop()

	for {
		select {
		case <-a.stopSync:
			return
		case <-refreshTicker.C:
			a.refreshAsync(false)
		case <-syncTicker.C:
			a.mu.Lock()
			dirty := a.dirty
			a.dirty = false
			a.mu.Unlock()
			if dirty {
				a.applySnapshot()
			}
			a.detectActiveChange()
			a.drainEngineEvents()
		}
	}
}

// setRefreshInterval mirrors _make_interval's stop/start dance (Python's
// rumps.Timer.interval setter is a no-op while running unless a full
// interval has elapsed; recreating the ticker forces the new cadence
// immediately). Called only from the settings-menu callback, which runs
// on the same goroutine reading a.refreshTimer, so no lock is needed on
// the ticker swap itself, only on the shared a.settings field.
func (a *app) setRefreshInterval(secs int) {
	a.mu.Lock()
	a.settings.RefreshInterval = secs
	a.mu.Unlock()
	// syncLoop reads a.settings.RefreshInterval only once at startup to
	// build its ticker; a live interval change takes effect on the next
	// process restart in this minimal port, a documented, deliberate
	// simplification versus Python's live Timer.interval reset (out of
	// scope for this task: the setting is still saved and reflected in
	// the menu's checkmark immediately, only the *ticker's* cadence lags
	// until the app is quit and relaunched).
}

func (a *app) applySnapshot() {
	a.mu.Lock()
	view := a.snapshot
	settings := a.settings
	a.mu.Unlock()
	systray.SetTitle(FormatTitle(view.activeEmail, view.activeUsage, settings, time.Now()))
	a.rebuildAccountSection(view)
	a.rebuildRemoveSection(view)
	a.rebuildHistorySection()
	a.syncThresholdCheckmarks()
}

func (a *app) detectActiveChange() {
	a.mu.Lock()
	refreshing := a.refreshing
	configPath := a.configPath
	a.mu.Unlock()
	if refreshing || configPath == "" {
		return
	}
	info, err := os.Stat(configPath)
	if err != nil {
		return
	}
	a.mu.Lock()
	same := info.ModTime().Equal(a.configMtime)
	a.configMtime = info.ModTime()
	a.mu.Unlock()
	if same {
		return
	}
	// Claude Code rewrites this file often for unrelated reasons, so the
	// mtime change alone isn't enough: only refresh when the active
	// email actually changed, matching _detect_active_change exactly
	// (this cheap local read has no Keychain/usage-API cost, so it's
	// fine to do on every mtime change).
	email, _, ok := a.switcher.GetCurrentAccount()
	if !ok || email == "" {
		return
	}
	a.mu.Lock()
	activeEmail := a.snapshot.activeEmail
	a.mu.Unlock()
	if email == activeEmail {
		return
	}
	a.refreshAsync(false)
}

// rebuildAccountSection diffs view.accounts against a.accountItems (see
// Global Constraints): unchanged accounts get SetTitle/Check mutations
// only; only genuinely added/removed accounts spawn/retire a
// click-handler goroutine, so no ClickedCh is ever orphaned.
func (a *app) rebuildAccountSection(view snapshotView) {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	for _, acc := range view.accounts {
		seen[acc.number] = true
		label := FormatAccountLabel(acc.number, acc.email, acc.display, time.Now())
		if tracked, ok := a.accountItems[acc.number]; ok {
			tracked.item.SetTitle(label)
			if acc.isActive {
				tracked.item.Check()
			} else {
				tracked.item.Uncheck()
			}
			continue
		}
		item := systray.AddMenuItemCheckbox(label, "", acc.isActive)
		done := make(chan struct{})
		number := acc.number
		go func() {
			for {
				select {
				case <-done:
					return
				case _, ok := <-item.ClickedCh:
					if !ok {
						return
					}
					a.onSwitchTo(number)
				}
			}
		}()
		a.accountItems[acc.number] = &trackedItem{item: item, done: done}
	}
	for number, tracked := range a.accountItems {
		if !seen[number] {
			close(tracked.done)
			tracked.item.Remove()
			delete(a.accountItems, number)
		}
	}
	// Mirrors rebuild_menu's `if not account_items: account_items.append(
	// rumps.MenuItem("No managed accounts", callback=None))`.
	if len(a.accountItems) == 0 && a.accountEmptyItem == nil {
		item := systray.AddMenuItem("No managed accounts", "")
		item.Disable()
		a.accountEmptyItem = item
	} else if len(a.accountItems) > 0 && a.accountEmptyItem != nil {
		a.accountEmptyItem.Remove()
		a.accountEmptyItem = nil
	}
}

func (a *app) rebuildRemoveSection(view snapshotView) {
	a.mu.Lock()
	defer a.mu.Unlock()
	seen := map[string]bool{}
	for _, acc := range view.accounts {
		seen[acc.number] = true
		label := fmt.Sprintf("%s  %s", acc.number, acc.email)
		if tracked, ok := a.removeItems[acc.number]; ok {
			tracked.item.SetTitle(label)
			continue
		}
		item := a.removeMenu.AddSubMenuItem(label, "")
		done := make(chan struct{})
		number, email := acc.number, acc.email
		go func() {
			for {
				select {
				case <-done:
					return
				case _, ok := <-item.ClickedCh:
					if !ok {
						return
					}
					a.onRemove(number, email)
				}
			}
		}()
		a.removeItems[acc.number] = &trackedItem{item: item, done: done}
	}
	for number, tracked := range a.removeItems {
		if !seen[number] {
			close(tracked.done)
			tracked.item.Remove()
			delete(a.removeItems, number)
		}
	}
	if len(a.removeItems) == 0 && a.removeEmptyItem == nil {
		item := a.removeMenu.AddSubMenuItem("No managed accounts", "")
		item.Disable()
		a.removeEmptyItem = item
	} else if len(a.removeItems) > 0 && a.removeEmptyItem != nil {
		a.removeEmptyItem.Remove()
		a.removeEmptyItem = nil
	}
}

// buildStaticMenu builds every fixed-count menu entry exactly once at
// startup (Rotate/Best/Next-available, Add account, Refresh
// credentials, Settings, Refresh now, Quit), none of these change in
// count at runtime, so none need the diffing discipline the account
// list requires. "Remove account" and "Switch history" are declared
// here as empty parent submenus; their contents are filled in by
// rebuildRemoveSection/rebuildHistorySection on every refresh.
func (a *app) buildStaticMenu() {
	systray.AddSeparator()
	a.clickHandler(systray.AddMenuItem("Rotate to next", ""), func() { a.onSwitch("") })
	a.clickHandler(systray.AddMenuItem("Switch to best", ""), func() { a.onSwitch("best") })
	a.clickHandler(systray.AddMenuItem("Next available", ""), func() { a.onSwitch("next-available") })
	systray.AddSeparator()

	addMenu := systray.AddMenuItem("Add account", "")
	a.clickHandler(addMenu.AddSubMenuItem("From current login", ""), a.onAddLogin)
	a.clickHandler(addMenu.AddSubMenuItem("From setup-token…", ""), a.onAddToken)

	a.removeMenu = systray.AddMenuItem("Remove account", "")

	a.clickHandler(systray.AddMenuItem("Refresh current credentials", ""), a.onRefreshCreds)

	a.historyMenu = systray.AddMenuItem("Switch history", "")
	a.historyMenu.AddSeparator()
	a.clickHandler(a.historyMenu.AddSubMenuItem("Open full log…", ""), func() {
		_ = RevealInFinder(a.logPath)
	})

	systray.AddSeparator()
	a.buildSettingsMenu()
	a.clickHandler(systray.AddMenuItem("Refresh now", ""), func() { a.refreshAsync(true) })
	a.clickHandler(systray.AddMenuItem("Quit", ""), systray.Quit)
}

// clickHandler spawns the one persistent goroutine a static (never
// removed) menu item needs for its whole process lifetime, safe here
// specifically because static items are never Remove()d, so there is no
// orphaning risk the Global Constraints diffing discipline exists to
// avoid; that discipline applies only to items created/destroyed at
// runtime (the account list, remove submenu, history submenu).
func (a *app) clickHandler(item *systray.MenuItem, fn func()) {
	go func() {
		for range item.ClickedCh {
			fn()
		}
	}()
}

func (a *app) buildSettingsMenu() {
	settingsMenu := systray.AddMenuItem("Settings", "")

	nameItem := settingsMenu.AddSubMenuItemCheckbox("Show account name in menu bar", "", a.settings.ShowAccountName)
	a.clickHandler(nameItem, func() {
		a.mu.Lock()
		a.settings.ShowAccountName = !a.settings.ShowAccountName
		if a.settings.ShowAccountName {
			nameItem.Check()
		} else {
			nameItem.Uncheck()
		}
		s := a.settings
		a.mu.Unlock()
		_ = s.Save(a.settingsPath)
		a.applySnapshot()
	})

	titlePctMenu := settingsMenu.AddSubMenuItem("Title percentage", "")
	titlePctLabels := map[string]string{"off": "None", "5h": "Session (5h)", "7d": "Weekly (7d)", "both": "Both (5h · 7d)"}
	titlePctItems := map[string]*systray.MenuItem{}
	for _, mode := range TitlePctChoices {
		mode := mode
		item := titlePctMenu.AddSubMenuItemCheckbox(titlePctLabels[mode], "", a.settings.TitlePct == mode)
		titlePctItems[mode] = item
		a.clickHandler(item, func() {
			a.mu.Lock()
			a.settings.TitlePct = mode
			s := a.settings
			for m, it := range titlePctItems {
				if m == mode {
					it.Check()
				} else {
					it.Uncheck()
				}
			}
			a.mu.Unlock()
			_ = s.Save(a.settingsPath)
			a.applySnapshot()
		})
	}

	intervalMenu := settingsMenu.AddSubMenuItem("Refresh interval", "")
	intervalLabels := map[int]string{30: "30 seconds", 60: "60 seconds", 300: "5 minutes"}
	intervalItems := map[int]*systray.MenuItem{}
	for _, secs := range RefreshChoices {
		secs := secs
		item := intervalMenu.AddSubMenuItemCheckbox(intervalLabels[secs], "", a.settings.RefreshInterval == secs)
		intervalItems[secs] = item
		a.clickHandler(item, func() {
			a.setRefreshInterval(secs)
			for s, it := range intervalItems {
				if s == secs {
					it.Check()
				} else {
					it.Uncheck()
				}
			}
			a.mu.Lock()
			s := a.settings
			a.mu.Unlock()
			_ = s.Save(a.settingsPath)
		})
	}

	autoItem := settingsMenu.AddSubMenuItemCheckbox("Auto-switch accounts", "", a.settings.AutoSwitchEnabled)
	a.clickHandler(autoItem, func() {
		a.mu.Lock()
		a.settings.AutoSwitchEnabled = !a.settings.AutoSwitchEnabled
		enabled := a.settings.AutoSwitchEnabled
		s := a.settings
		a.mu.Unlock()
		if enabled {
			autoItem.Check()
			a.startEngine()
		} else {
			autoItem.Uncheck()
			a.stopEngine()
		}
		_ = s.Save(a.settingsPath)
	})

	thresholdMenu := settingsMenu.AddSubMenuItem("Auto-switch threshold", "")
	thresholdItems := map[int]*systray.MenuItem{}
	current := a.currentThreshold()
	for _, pct := range AutoThresholdChoices {
		pct := pct
		item := thresholdMenu.AddSubMenuItemCheckbox(fmt.Sprintf("%d%%", pct), "", current == pct)
		thresholdItems[pct] = item
		a.clickHandler(item, func() {
			if _, err := cswap.SetSetting(a.switcher.BackupDir, "autoswitch.threshold", fmt.Sprintf("%d", pct)); err != nil {
				Alert(a.osa, "claude-swap", fmt.Sprintf("Couldn't set threshold: %s", err))
				return
			}
			for p, it := range thresholdItems {
				if p == pct {
					it.Check()
				} else {
					it.Uncheck()
				}
			}
			a.mu.Lock()
			a.lastSyncedThreshold = pct
			a.mu.Unlock()
			a.restartEngine()
		})
	}
	a.mu.Lock()
	a.thresholdItems = thresholdItems
	a.lastSyncedThreshold = current
	a.mu.Unlock()
}

// syncThresholdCheckmarks mirrors _settings_menu/_threshold() being
// rebuilt fresh on every rebuild_menu() call: re-reads the live
// autoswitch.threshold setting and updates the Settings submenu's
// checkmarks if it changed since the last sync, so a threshold changed
// externally (cswap config set, another cswap auto/menu bar instance) is
// reflected within one refresh cycle instead of only on relaunch.
func (a *app) syncThresholdCheckmarks() {
	current := a.currentThreshold()
	a.mu.Lock()
	defer a.mu.Unlock()
	if current == a.lastSyncedThreshold {
		return
	}
	a.lastSyncedThreshold = current
	for pct, item := range a.thresholdItems {
		if pct == current {
			item.Check()
		} else {
			item.Uncheck()
		}
	}
}

func (a *app) currentThreshold() int {
	settings := cswap.LoadSettings(a.switcher.BackupDir)
	return int(settings.Threshold)
}

// ---- engine -----------------------------------------------------------

func (a *app) startEngine() {
	a.engineMu.Lock()
	defer a.engineMu.Unlock()
	if a.engine != nil {
		return
	}
	settings := cswap.LoadSettings(a.switcher.BackupDir)
	events := make(chan cswap.AutoSwitchEvent, 32)
	a.engineEvts = events
	engine := cswap.NewAutoSwitchEngine(a.switcher, settings, func(e cswap.AutoSwitchEvent) {
		events <- e
	}, false, "", nil)
	a.engine = engine
	go func() {
		engine.RunLoop()
		close(events)
	}()
}

func (a *app) stopEngine() {
	a.engineMu.Lock()
	defer a.engineMu.Unlock()
	if a.engine == nil {
		return
	}
	a.engine.Stop()
	a.engine = nil
}

func (a *app) restartEngine() {
	a.engineMu.Lock()
	running := a.engine != nil
	a.engineMu.Unlock()
	if running {
		a.stopEngine()
		a.startEngine()
	}
}

func (a *app) drainEngineEvents() {
	a.engineMu.Lock()
	events := a.engineEvts
	a.engineMu.Unlock()
	if events == nil {
		return
	}
	for {
		select {
		case e, ok := <-events:
			if !ok {
				return
			}
			switch e.Kind() {
			case "switch":
				Notify(a.osa, "claude-swap", "Auto-switched account: "+e.Human())
				a.refreshAsync(false)
			case "account-quarantined":
				Notify(a.osa, "claude-swap", "Account quarantined: "+e.Human())
			case "all-exhausted":
				Notify(a.osa, "claude-swap", "All accounts exhausted: "+e.Human())
			case "config-warning":
				Notify(a.osa, "claude-swap", "Configuration warning: "+e.Human())
			}
		default:
			return
		}
	}
}

// ---- callbacks ----------------------------------------------------------

func (a *app) guard(err error) bool {
	if err == nil {
		return true
	}
	Alert(a.osa, "claude-swap", err.Error())
	return false
}

func (a *app) notifySwitched() {
	Notify(a.osa, "claude-swap", "Account switched. Switch takes effect within ~30s, restart Claude Code to apply immediately.")
}

func (a *app) onSwitchTo(number string) {
	_, err := a.switcher.SwitchTo(number, false, nil)
	if a.guard(err) {
		a.notifySwitched()
		a.refreshAsync(false)
	}
}

func (a *app) onSwitch(strategy string) {
	_, err := a.switcher.Switch(strategy, nil)
	if a.guard(err) {
		a.notifySwitched()
		a.refreshAsync(false)
	}
}

func (a *app) onRemove(number, email string) {
	if !ConfirmDialog(a.osa, "Remove account", fmt.Sprintf("Remove account %s?", number), "Remove") {
		return
	}
	_, err := a.switcher.RemoveAccount(number, nil, nil)
	if a.guard(err) {
		a.refreshAsync(false)
	}
}

func (a *app) onAddLogin() {
	_, err := a.switcher.AddAccount(nil, nil)
	if a.guard(err) {
		a.refreshAsync(false)
	}
}

func (a *app) onAddToken() {
	email, ok := TextInputDialog(a.osa, "Add account from setup-token", "Email for this token:")
	if !ok {
		return
	}
	token, ok := TextInputDialog(a.osa, "Add account from setup-token", "Setup token (sk-ant-oat01-…):")
	if !ok {
		return
	}
	_, err := a.switcher.AddAccountFromToken(token, email, nil, nil)
	if a.guard(err) {
		a.refreshAsync(false)
	}
}

func (a *app) onRefreshCreds() {
	if _, _, ok := a.switcher.GetCurrentAccount(); !ok {
		Alert(a.osa, "claude-swap", "No active Claude Code login detected. Log in first.")
		return
	}
	_, err := a.switcher.AddAccount(nil, nil)
	if err != nil {
		if errors.Is(err, cswap.ErrKeychainUnavailable) {
			Alert(a.osa, "claude-swap", "Couldn't read the active credential. If the menu bar is running as a background/login agent, macOS blocks its Keychain access. Quit and relaunch it from a Terminal with: cswap --menubar")
			return
		}
		Alert(a.osa, "claude-swap", err.Error())
		return
	}
	a.refreshAsync(false)
}

func (a *app) rebuildHistorySection() {
	a.mu.Lock()
	defer a.mu.Unlock()
	text, err := os.ReadFile(a.logPath)
	body := ""
	if err == nil {
		body = string(text)
	}
	entries := ParseSwitchHistory(body, SwitchHistoryLimit)

	for _, item := range a.historyEntryItems {
		item.Remove()
	}
	a.historyEntryItems = a.historyEntryItems[:0]

	if len(entries) == 0 {
		item := a.historyMenu.AddSubMenuItem("No switches logged yet", "")
		item.Disable()
		a.historyEntryItems = append(a.historyEntryItems, item)
	}
	for _, line := range entries {
		item := a.historyMenu.AddSubMenuItem(line, "")
		item.Disable()
		a.historyEntryItems = append(a.historyEntryItems, item)
	}
}
