package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	blockpkg "github.com/deroproject/derohe/block"
	"github.com/deroproject/derohe/config"
	"github.com/deroproject/derohe/cryptography/crypto"
	"github.com/deroproject/derohe/globals"
	"github.com/deroproject/derohe/rpc"
	"github.com/deroproject/derohe/transaction"
	"github.com/deroproject/derohe/walletapi"
)

//go:embed public/*
var publicFiles embed.FS

const (
	defaultClientPort         = "8797"
	daemonRPCPort             = "40412"
	getworkPort               = "40410"
	publicSeedNode            = "150.158.101.65:40411"
	registrationConfirmations = int64(3)
	minerAddressMaturity      = int64(25)
)

type app struct {
	mu sync.Mutex

	dataDir      string
	runtimeBin   string
	logDir       string
	walletFile   string
	passwordFile string
	daemonRPC    string
	getwork      string

	node         *managedProcess
	miner        *managedProcess
	minerThreads int

	wallet *walletapi.Wallet_Disk
	logs   *ringLog

	registration registrationState
	pendingTxs   []walletTx
}

type managedProcess struct {
	name      string
	cmd       *exec.Cmd
	started   time.Time
	target    string
	exitErr   error
	exited    bool
	logHandle *os.File
}

type registrationState struct {
	Running   bool      `json:"running"`
	Done      bool      `json:"done"`
	TXID      string    `json:"txid,omitempty"`
	LastError string    `json:"lastError,omitempty"`
	Started   time.Time `json:"started,omitempty"`
}

type explorerBlock struct {
	TopoHeight   int64        `json:"topoheight"`
	Height       int64        `json:"height"`
	Hash         string       `json:"hash"`
	Timestamp    uint64       `json:"timestamp"`
	Reward       string       `json:"reward"`
	TXCount      int          `json:"txcount"`
	MiniBlocks   int          `json:"miniblocks"`
	Transactions []explorerTx `json:"transactions"`
}

type explorerTx struct {
	Hash        string `json:"hash"`
	Type        string `json:"type"`
	Size        int    `json:"size"`
	Fees        string `json:"fees,omitempty"`
	Burn        string `json:"burn,omitempty"`
	Payloads    int    `json:"payloads,omitempty"`
	RingSize    uint64 `json:"ringSize,omitempty"`
	BlockHeight int64  `json:"blockHeight,omitempty"`
	ValidBlock  string `json:"validBlock,omitempty"`
	InPool      bool   `json:"inPool,omitempty"`
	SCAction    string `json:"scAction,omitempty"`
	SCID        string `json:"scid,omitempty"`
	CodeSize    int    `json:"codeSize,omitempty"`
	Note        string `json:"note,omitempty"`
}

type walletTx struct {
	Kind         string                 `json:"kind"`
	TXID         string                 `json:"txid"`
	BlockHash    string                 `json:"blockhash"`
	Height       uint64                 `json:"height"`
	TopoHeight   int64                  `json:"topoheight"`
	Time         string                 `json:"time"`
	Amount       string                 `json:"amount"`
	Fees         string                 `json:"fees,omitempty"`
	Counterparty string                 `json:"counterparty,omitempty"`
	Message      string                 `json:"message,omitempty"`
	Pending      bool                   `json:"pending,omitempty"`
	PayloadArgs  []map[string]string    `json:"payloadArgs,omitempty"`
	Raw          map[string]interface{} `json:"raw,omitempty"`
}

type ringLog struct {
	mu    sync.Mutex
	lines []string
	max   int
}

func newRingLog(max int) *ringLog {
	return &ringLog{max: max}
}

func (l *ringLog) add(prefix string, p []byte) {
	scanner := bytes.Split(p, []byte{'\n'})
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, raw := range scanner {
		line := sanitizeLogLine(strings.TrimSpace(string(raw)))
		if line == "" || isPromptNoise(line) {
			continue
		}
		l.lines = append(l.lines, fmt.Sprintf("%s [%s] %s", time.Now().Format("15:04:05"), prefix, line))
		if len(l.lines) > l.max {
			l.lines = l.lines[len(l.lines)-l.max:]
		}
	}
}

func sanitizeLogLine(line string) string {
	var out strings.Builder
	skipEscape := false
	for i := 0; i < len(line); i++ {
		b := line[i]
		if skipEscape {
			if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') {
				skipEscape = false
			}
			continue
		}
		if b == 0x1b {
			skipEscape = true
			continue
		}
		if b < 32 && b != '\t' {
			continue
		}
		out.WriteByte(b)
	}
	return strings.TrimSpace(out.String())
}

func isPromptNoise(line string) bool {
	return strings.HasPrefix(line, "DERO HE:") ||
		strings.HasPrefix(line, "DERO Miner:") ||
		strings.HasPrefix(line, "DERO:") ||
		line == ">>>" ||
		line == ">"
}

func (l *ringLog) snapshot() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, len(l.lines))
	copy(out, l.lines)
	return out
}

type logWriter struct {
	logs   *ringLog
	prefix string
}

func (w logWriter) Write(p []byte) (int, error) {
	w.logs.add(w.prefix, p)
	return len(p), nil
}

func main() {
	port := flag.String("port", envDefault("CATFISH_DERO_PORT", defaultClientPort), "local web UI port")
	noBrowser := flag.Bool("no-browser", false, "do not open browser")
	dataDirFlag := flag.String("data-dir", "", "data directory")
	flag.Parse()

	dataDir, err := resolveDataDir(*dataDirFlag)
	if err != nil {
		fatal(err)
	}

	a := &app{
		dataDir:      dataDir,
		runtimeBin:   filepath.Join(dataDir, "bin"),
		logDir:       filepath.Join(dataDir, "logs"),
		walletFile:   filepath.Join(dataDir, "wallet", "wallet.db"),
		passwordFile: filepath.Join(dataDir, "wallet", "wallet.pass"),
		daemonRPC:    net.JoinHostPort("127.0.0.1", daemonRPCPort),
		getwork:      net.JoinHostPort("127.0.0.1", getworkPort),
		logs:         newRingLog(1200),
	}

	if err := a.prepare(); err != nil {
		fatal(err)
	}
	if err := a.startNode(context.Background()); err != nil {
		a.logs.add("client", []byte("node start failed: "+err.Error()))
	}
	if err := a.openWallet(); err != nil {
		a.logs.add("client", []byte("wallet open failed: "+err.Error()))
	}
	go a.monitorLoop()

	mux := http.NewServeMux()
	a.routes(mux)

	srv := &http.Server{Addr: "127.0.0.1:" + *port, Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		a.close()
	}()

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			a.logs.add("client", []byte("web server failed: "+err.Error()))
		}
	}()

	if err := waitHTTP(*port, 10*time.Second); err == nil {
		if !*noBrowser {
			openBrowser("http://127.0.0.1:" + *port)
		}
		fmt.Printf("Catfish DERO Desktop: http://127.0.0.1:%s\n", *port)
		fmt.Printf("Data: %s\n", dataDir)
	} else {
		fatal(err)
	}

	<-ctx.Done()
}

func (a *app) prepare() error {
	for _, dir := range []string{a.dataDir, a.runtimeBin, a.logDir, filepath.Dir(a.walletFile)} {
		if err := os.MkdirAll(dir, 0750); err != nil {
			return err
		}
	}

	globals.Arguments = map[string]interface{}{
		"--testnet":        true,
		"--remote":         false,
		"--daemon-address": a.daemonRPC,
		"--socks-proxy":    nil,
		"--simulator":      false,
	}
	globals.InitNetwork()
	walletapi.SetDaemonAddress(a.daemonRPC)
	walletapi.Initialize_LookupTable(1, 1<<21)
	go walletapi.Keep_Connectivity()

	return a.ensureRuntimeBinaries()
}

func (a *app) ensureRuntimeBinaries() error {
	names := []string{"derod", "dero-miner", "dero-wallet-cli"}
	for _, name := range names {
		src, err := findBundledBinary(name)
		if err != nil {
			if name == "dero-wallet-cli" {
				continue
			}
			return err
		}
		dst := filepath.Join(a.runtimeBin, exeName(name))
		if err := copyIfNeeded(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func (a *app) openWallet() error {
	password, err := a.walletPassword()
	if err != nil {
		return err
	}

	var wallet *walletapi.Wallet_Disk
	if _, err := os.Stat(a.walletFile); os.IsNotExist(err) {
		account, err := walletapi.Generate_Keys_From_Random()
		if err != nil {
			return err
		}
		wallet, err = walletapi.Create_Encrypted_Wallet(a.walletFile, password, account.Keys.Secret)
		if err != nil {
			return err
		}
		wallet.SetSeedLanguage("English")
		if err := wallet.Save_Wallet(); err != nil {
			return err
		}
		a.logs.add("wallet", []byte("created a new DERO wallet"))
	} else {
		wallet, err = walletapi.Open_Encrypted_Wallet(a.walletFile, password)
		if err != nil {
			return err
		}
		a.logs.add("wallet", []byte("opened existing DERO wallet"))
	}

	wallet.SetDaemonAddress(a.daemonRPC)
	wallet.SetNetwork(false)
	wallet.SetRingSize(2)
	wallet.SetSaveDuration(30 * time.Second)
	wallet.SetOnlineMode()
	a.wallet = wallet
	return nil
}

func (a *app) walletPassword() (string, error) {
	if data, err := os.ReadFile(a.passwordFile); err == nil {
		return strings.TrimSpace(string(data)), nil
	}
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	password := hex.EncodeToString(b[:])
	if err := os.WriteFile(a.passwordFile, []byte(password), 0600); err != nil {
		return "", err
	}
	return password, nil
}

func (a *app) startNode(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.node != nil && a.node.running() {
		return nil
	}

	args := []string{
		"--testnet",
		"--data-dir=" + filepath.Join(a.dataDir, "chain"),
		"--rpc-bind=" + a.daemonRPC,
		"--getwork-bind=" + a.getwork,
		// Listen on an ephemeral UDP port (omit --p2p-bind=:0 which disables P2P).
		// STUN then maps this socket's public ip:port for peer dialing.
		"--add-priority-node=" + publicSeedNode,
		"--min-peers=2",
		"--max-peers=32",
		"--timeisinsync",
		"--fastsync",
		"--node-tag=MoefishDero",
		"--log-dir=" + a.logDir,
	}
	env := append(os.Environ(),
		"JOB_SEND_TIME_DELAY=500ms",
		"NO_PROXY=127.0.0.1,localhost",
		"no_proxy=127.0.0.1,localhost",
	)
	proc, err := a.startProcess(ctx, "node", filepath.Join(a.runtimeBin, exeName("derod")), args, env, "")
	if err != nil {
		return err
	}
	a.node = proc
	a.logs.add("client", []byte("Moefish public node is starting; seed="+publicSeedNode))
	return nil
}

func (a *app) restartNode(ctx context.Context) error {
	a.mu.Lock()
	if a.node != nil {
		_ = a.node.stop()
		a.node = nil
	}
	a.mu.Unlock()
	return a.startNode(ctx)
}

func (a *app) startMiner(ctx context.Context, threads int) error {
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if wallet == nil {
		return fmt.Errorf("wallet is not ready")
	}
	if threads < 1 {
		threads = max(1, runtime.NumCPU()/2)
	}
	if threads > runtime.NumCPU() {
		threads = runtime.NumCPU()
	}

	target := wallet.GetAddress().String()
	mode := "wallet"
	if !wallet.IsRegistered() {
		a.startRegistration()
		target = config.Testnet.Dev_Address
		mode = "registration-bootstrap"
	} else if err := a.ensureMinerAddressReady(wallet); err != nil {
		target = config.Testnet.Dev_Address
		mode = "maturity-bootstrap"
		a.logs.add("miner", []byte("wallet miner address is not mature; mining temporarily to bootstrap address: "+err.Error()))
	}

	a.mu.Lock()
	defer a.mu.Unlock()
	if a.miner != nil && a.miner.running() {
		_ = a.miner.stop()
	}
	args := []string{
		"--testnet",
		"--wallet-address=" + target,
		"--daemon-rpc-address=" + a.getwork,
		"--mining-threads=" + strconv.Itoa(threads),
	}
	env := append(os.Environ(),
		"NO_PROXY=127.0.0.1,localhost",
		"no_proxy=127.0.0.1,localhost",
	)
	proc, err := a.startProcess(ctx, "miner", filepath.Join(a.runtimeBin, exeName("dero-miner")), args, env, mode)
	if err != nil {
		return err
	}
	a.miner = proc
	a.minerThreads = threads
	a.logs.add("miner", []byte("started miner target="+target+" mode="+mode))
	return nil
}

func (a *app) ensureMinerAddressReady(wallet *walletapi.Wallet_Disk) error {
	if wallet == nil {
		return fmt.Errorf("钱包还没有准备好，请稍后重试")
	}
	var info rpc.GetInfo_Result
	if err := a.daemonCall("get_info", nil, &info); err != nil {
		return fmt.Errorf("节点还没有准备好，无法确认挖矿地址状态: %w", err)
	}
	registrationTopo := wallet.Get_Registration_TopoHeight()
	if registrationTopo < 0 {
		return fmt.Errorf("当前钱包还没有上链注册，暂时不能作为挖矿地址；请先完成注册")
	}

	remaining := minerAddressMaturityRemaining(info.TopoHeight, registrationTopo)
	if remaining > 0 {
		return fmt.Errorf("当前钱包已注册，但挖矿地址还在成熟等待期；还需要大约 %d 个区块后才能把挖矿奖励切回当前钱包（当前 topoheight=%d，注册 topoheight=%d）", remaining, info.TopoHeight, registrationTopo)
	}
	return nil
}

func (a *app) stopMiner() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.miner == nil {
		return nil
	}
	err := a.miner.stop()
	a.miner = nil
	a.logs.add("miner", []byte("stopped miner"))
	return err
}

func (a *app) startProcess(ctx context.Context, name, path string, args []string, env []string, target string) (*managedProcess, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%s binary not found: %w", name, err)
	}
	logPath := filepath.Join(a.logDir, name+".log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0640)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = a.dataDir
	cmd.Env = env
	out := io.MultiWriter(logFile, logWriter{logs: a.logs, prefix: name})
	cmd.Stdout = out
	cmd.Stderr = out
	setProcessAttrs(cmd)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	proc := &managedProcess{name: name, cmd: cmd, started: time.Now(), logHandle: logFile, target: target}
	go func() {
		err := cmd.Wait()
		proc.exitErr = err
		proc.exited = true
		_ = logFile.Close()
		if err != nil {
			a.logs.add(name, []byte("exited: "+err.Error()))
		} else {
			a.logs.add(name, []byte("exited"))
		}
	}()
	return proc, nil
}

func (p *managedProcess) running() bool {
	return p != nil && p.cmd != nil && p.cmd.Process != nil && !p.exited
}

func (p *managedProcess) stop() error {
	if p == nil || p.cmd == nil || p.cmd.Process == nil || p.exited {
		return nil
	}
	if runtime.GOOS == "windows" {
		// Hide the brief console flash from taskkill.exe.
		kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.cmd.Process.Pid))
		setProcessAttrs(kill)
		_ = kill.Run()
	} else {
		_ = p.cmd.Process.Signal(os.Interrupt)
		time.Sleep(500 * time.Millisecond)
		if !p.exited {
			_ = p.cmd.Process.Kill()
		}
	}
	return nil
}

func (a *app) close() {
	_ = a.stopMiner()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.node != nil {
		_ = a.node.stop()
		a.node = nil
	}
	if a.wallet != nil {
		a.wallet.Close_Encrypted_Wallet()
		a.wallet = nil
	}
}

func (a *app) monitorLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		a.mu.Lock()
		wallet := a.wallet
		miner := a.miner
		minerThreads := a.minerThreads
		a.mu.Unlock()
		if wallet != nil && wallet.GetMode() {
			_ = wallet.Sync_Wallet_Memory_With_Daemon()
		}
		if wallet != nil && miner != nil && miner.running() && isBootstrapMinerTarget(miner.target) {
			if wallet.IsRegistered() {
				if err := a.ensureMinerAddressReady(wallet); err == nil {
					a.logs.add("miner", []byte("wallet miner address is ready; switching miner reward address to wallet"))
					_ = a.startMiner(context.Background(), minerThreads)
				}
			}
		}
	}
}

func isBootstrapMinerTarget(target string) bool {
	return target == "registration-bootstrap" || target == "maturity-bootstrap"
}

func (a *app) startRegistration() {
	a.mu.Lock()
	if a.registration.Running || a.wallet == nil || a.wallet.IsRegistered() {
		a.mu.Unlock()
		return
	}
	a.registration = registrationState{Running: true, Started: time.Now()}
	wallet := a.wallet
	a.mu.Unlock()

	a.logs.add("wallet", []byte("registration proof-of-work started"))
	go func() {
		tx, err := buildRegistrationTX(wallet)
		a.mu.Lock()
		defer a.mu.Unlock()
		if err != nil {
			a.registration.Running = false
			a.registration.LastError = err.Error()
			a.logs.add("wallet", []byte("registration failed: "+err.Error()))
			return
		}
		if err := wallet.SendTransaction(tx); err != nil {
			a.registration.Running = false
			a.registration.LastError = err.Error()
			a.logs.add("wallet", []byte("registration submit failed: "+err.Error()))
			return
		}
		a.registration.Running = false
		a.registration.Done = true
		a.registration.TXID = tx.GetHash().String()
		a.logs.add("wallet", []byte("registration tx submitted: "+a.registration.TXID))
	}()
}

func buildRegistrationTX(wallet *walletapi.Wallet_Disk) (*transaction.Transaction, error) {
	if wallet == nil {
		return nil, fmt.Errorf("wallet is not ready")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan *transaction.Transaction, 1)
	workers := max(1, runtime.NumCPU()/2)
	for i := 0; i < workers; i++ {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}
				tx := wallet.GetRegistrationTX()
				hash := tx.GetHash()
				if hash[0] == 0 && hash[1] == 0 {
					select {
					case ch <- tx:
						cancel()
					default:
					}
					return
				}
			}
		}()
	}
	select {
	case tx := <-ch:
		return tx, nil
	case <-time.After(10 * time.Minute):
		return nil, fmt.Errorf("registration proof-of-work timed out")
	}
}

func (a *app) routes(mux *http.ServeMux) {
	static, _ := fs.Sub(publicFiles, "public")
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/desktop", a.handleDesktop)
	mux.HandleFunc("/api/status", a.handleStatus)
	mux.HandleFunc("/api/logs", a.handleLogs)
	mux.HandleFunc("/api/node/restart", a.handleRestartNode)
	mux.HandleFunc("/api/miner/toggle", a.handleMinerToggle)
	mux.HandleFunc("/api/miner/start", a.handleMinerStart)
	mux.HandleFunc("/api/miner/stop", a.handleMinerStop)
	mux.HandleFunc("/api/wallet/refresh", a.handleWalletRefresh)
	mux.HandleFunc("/api/wallet/register", a.handleWalletRegister)
	mux.HandleFunc("/api/wallet/seed", a.handleWalletSeed)
	mux.HandleFunc("/api/transfer", a.handleTransfer)
	mux.HandleFunc("/api/contracts/install", a.handleContractInstall)
	mux.HandleFunc("/api/contracts/call", a.handleContractCall)
	mux.HandleFunc("/api/contracts/query", a.handleContractQuery)
	mux.HandleFunc("/api/blocks", a.handleBlocks)
	mux.HandleFunc("/api/wallet/transactions", a.handleWalletTransactions)
}

func (a *app) handleDesktop(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{
		"name":      "Catfish DERO Desktop",
		"dataDir":   a.dataDir,
		"daemonRPC": a.daemonRPC,
		"getwork":   a.getwork,
	})
}

func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) {
	info := map[string]interface{}{}
	var daemonInfo rpc.GetInfo_Result
	daemonOnline := false
	if err := a.daemonCall("get_info", nil, &daemonInfo); err != nil {
		info["online"] = false
		info["error"] = err.Error()
	} else {
		daemonOnline = true
		info["online"] = true
		info["height"] = daemonInfo.Height
		info["topoheight"] = daemonInfo.TopoHeight
		info["difficulty"] = daemonInfo.Difficulty
		info["miners"] = daemonInfo.Miners
		info["incomingPeers"] = daemonInfo.Incoming_connections_count
		info["outgoingPeers"] = daemonInfo.Outgoing_connections_count
		info["peers"] = daemonInfo.Incoming_connections_count + daemonInfo.Outgoing_connections_count
		info["blocks"] = daemonInfo.CountBlocks
		info["miniblocks"] = daemonInfo.CountMinisAccepted
		info["rejected"] = daemonInfo.CountMinisRejected
		info["network"] = "catfish-public"
		var connections rpc.GetConnections_Result
		if err := a.daemonCall("get_connections", nil, &connections); err == nil {
			info["connectionList"] = connections.Connections
			info["localPort"] = connections.Local_Port
			info["advertisedPort"] = connections.Advertised_Port
			info["externalAddress"] = connections.External_Address
			if connections.Incoming_connections_count+connections.Outgoing_connections_count > 0 {
				info["incomingPeers"] = connections.Incoming_connections_count
				info["outgoingPeers"] = connections.Outgoing_connections_count
				info["peers"] = connections.Incoming_connections_count + connections.Outgoing_connections_count
			}
		} else {
			info["connectionList"] = []rpc.Connection_Info{}
		}
	}

	a.mu.Lock()
	wallet := a.wallet
	miner := a.miner
	node := a.node
	reg := a.registration
	a.mu.Unlock()

	walletInfo := map[string]interface{}{"ready": false}
	registrationView := map[string]interface{}{
		"running":                reg.Running,
		"done":                   reg.Done,
		"txid":                   reg.TXID,
		"lastError":              reg.LastError,
		"started":                reg.Started,
		"status":                 "unregistered",
		"spendable":              false,
		"confirmationsRequired":  registrationConfirmations,
		"confirmationsRemaining": 0,
	}
	if wallet != nil {
		registered := wallet.IsRegistered()
		registrationHeight := wallet.Get_Registration_TopoHeight()
		remaining := int64(0)
		minerMaturityRemaining := int64(0)
		registrationStatus := "unregistered"
		spendable := false
		minerReady := false
		if registered {
			remaining = registrationConfirmationsRemaining(daemonInfo.TopoHeight, registrationHeight)
			if daemonOnline {
				minerMaturityRemaining = minerAddressMaturityRemaining(daemonInfo.TopoHeight, registrationHeight)
				minerReady = minerMaturityRemaining == 0
			}
			if remaining > 0 {
				registrationStatus = "confirming"
			} else {
				registrationStatus = "spendable"
				spendable = true
			}
		} else if reg.Running {
			registrationStatus = "running"
		} else if reg.Done && reg.TXID != "" {
			registrationStatus = "submitted"
		}
		mature, locked := wallet.Get_Balance()
		walletInfo = map[string]interface{}{
			"ready":              true,
			"address":            wallet.GetAddress().String(),
			"registered":         registered,
			"registrationHeight": registrationHeight,
			"minerReady":         minerReady,
			"minerMaturity":      minerAddressMaturity,
			"minerMaturityLeft":  minerMaturityRemaining,
			"height":             wallet.Get_Height(),
			"daemonHeight":       wallet.Get_Daemon_Height(),
			"balanceAtomic":      mature + locked,
			"unlockedAtomic":     mature,
			"lockedAtomic":       locked,
			"balance":            rpc.FormatMoney(mature + locked),
			"unlocked":           rpc.FormatMoney(mature),
			"locked":             rpc.FormatMoney(locked),
		}
		registrationView["status"] = registrationStatus
		registrationView["spendable"] = spendable
		registrationView["registrationHeight"] = registrationHeight
		registrationView["confirmationsRemaining"] = remaining
	}

	writeJSON(w, map[string]interface{}{
		"node": map[string]interface{}{
			"running": node != nil && node.running(),
		},
		"daemon":       info,
		"wallet":       walletInfo,
		"registration": registrationView,
		"miner": map[string]interface{}{
			"running": miner != nil && miner.running(),
			"mode":    processTarget(miner),
		},
	})
}

func (a *app) handleLogs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]interface{}{"lines": a.logs.snapshot()})
}

func (a *app) handleRestartNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.restartNode(context.Background()); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (a *app) handleMinerToggle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	running := a.miner != nil && a.miner.running()
	a.mu.Unlock()
	if running {
		if err := a.stopMiner(); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, map[string]interface{}{"ok": true, "running": false})
		return
	}
	var req struct {
		Threads int `json:"threads"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := a.startMiner(context.Background(), req.Threads); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "running": true})
}

func (a *app) handleMinerStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Threads int `json:"threads"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if err := a.startMiner(context.Background(), req.Threads); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (a *app) handleMinerStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := a.stopMiner(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (a *app) handleWalletRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if wallet == nil {
		writeError(w, fmt.Errorf("wallet is not ready"))
		return
	}
	if err := wallet.Sync_Wallet_Memory_With_Daemon(); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (a *app) handleWalletRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.startRegistration()
	writeJSON(w, map[string]interface{}{"ok": true})
}

func (a *app) handleWalletSeed(w http.ResponseWriter, r *http.Request) {
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if wallet == nil {
		writeError(w, fmt.Errorf("wallet is not ready"))
		return
	}
	writeJSON(w, map[string]interface{}{"seed": wallet.GetSeed()})
}

func (a *app) handleTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Destination string `json:"destination"`
		Amount      string `json:"amount"`
		Memo        string `json:"memo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	amount, err := parseMoney(req.Amount)
	if err != nil {
		writeError(w, err)
		return
	}
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if err := a.ensureSpendableWallet(wallet); err != nil {
		writeError(w, err)
		return
	}
	destination := strings.TrimSpace(req.Destination)
	if err := a.preflightTransfer(wallet, destination); err != nil {
		writeError(w, err)
		return
	}
	payload := rpc.Arguments{}
	if memo := strings.TrimSpace(req.Memo); memo != "" {
		payload = append(payload, rpc.Argument{Name: rpc.RPC_COMMENT, DataType: rpc.DataString, Value: memo})
		if _, err := payload.CheckPack(transaction.PAYLOAD0_LIMIT); err != nil {
			writeError(w, fmt.Errorf("留言太长，请缩短后再发送: %w", err))
			return
		}
	}
	tx, err := wallet.TransferPayload0([]rpc.Transfer{{Destination: destination, Amount: amount, Payload_RPC: payload}}, 2, false, rpc.Arguments{}, 0, false)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := wallet.SendTransaction(tx); err != nil {
		writeError(w, err)
		return
	}
	txid := tx.GetHash().String()
	a.addPendingTransfer(txid, destination, amount, strings.TrimSpace(req.Memo))
	writeJSON(w, map[string]interface{}{"ok": true, "txid": txid})
}

func (a *app) handleContractInstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		Source string `json:"source"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if err := a.ensureSpendableWallet(wallet); err != nil {
		writeError(w, err)
		return
	}
	scdata := rpc.Arguments{
		{Name: rpc.SCACTION, DataType: rpc.DataUint64, Value: uint64(rpc.SC_INSTALL)},
		{Name: rpc.SCCODE, DataType: rpc.DataString, Value: req.Source},
	}
	tx, err := wallet.TransferPayload0(nil, 2, false, scdata, 0, false)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := wallet.SendTransaction(tx); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "txid": tx.GetHash().String(), "scid": tx.GetHash().String()})
}

func (a *app) handleContractCall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req struct {
		SCID        string        `json:"scid"`
		Entrypoint  string        `json:"entrypoint"`
		DERODeposit string        `json:"deroDeposit"`
		Args        []rpcArgument `json:"args"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, err)
		return
	}
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if err := a.ensureSpendableWallet(wallet); err != nil {
		writeError(w, err)
		return
	}
	scid := crypto.HashHexToHash(strings.TrimSpace(req.SCID))
	scdata := rpc.Arguments{
		{Name: rpc.SCACTION, DataType: rpc.DataUint64, Value: uint64(rpc.SC_CALL)},
		{Name: rpc.SCID, DataType: rpc.DataHash, Value: scid},
		{Name: "entrypoint", DataType: rpc.DataString, Value: strings.TrimSpace(req.Entrypoint)},
	}
	for _, arg := range req.Args {
		converted, err := arg.convert()
		if err != nil {
			writeError(w, err)
			return
		}
		scdata = append(scdata, converted)
	}
	var transfers []rpc.Transfer
	if strings.TrimSpace(req.DERODeposit) != "" {
		deposit, err := parseMoney(req.DERODeposit)
		if err != nil {
			writeError(w, err)
			return
		}
		if deposit > 0 {
			randomMembers := wallet.Random_ring_members(crypto.Hash{})
			if len(randomMembers) == 0 {
				writeError(w, fmt.Errorf("could not find a ring member for DERO deposit"))
				return
			}
			transfers = append(transfers, rpc.Transfer{Destination: randomMembers[0], Amount: 0, Burn: deposit})
		}
	}
	tx, err := wallet.TransferPayload0(transfers, 2, false, scdata, 0, false)
	if err != nil {
		writeError(w, err)
		return
	}
	if err := wallet.SendTransaction(tx); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]interface{}{"ok": true, "txid": tx.GetHash().String()})
}

func (a *app) handleContractQuery(w http.ResponseWriter, r *http.Request) {
	scid := strings.TrimSpace(r.URL.Query().Get("scid"))
	if scid == "" {
		writeError(w, fmt.Errorf("scid is required"))
		return
	}
	var result rpc.GetSC_Result
	err := a.daemonCall("getsc", rpc.GetSC_Params{SCID: scid, Code: true, Variables: true}, &result)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, result)
}

func (a *app) handleBlocks(w http.ResponseWriter, r *http.Request) {
	limit := 16
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 100 {
			limit = n
		}
	}
	var info rpc.GetInfo_Result
	if err := a.daemonCall("get_info", nil, &info); err != nil {
		writeError(w, err)
		return
	}
	startTopo := info.TopoHeight
	if raw := r.URL.Query().Get("start"); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			if n < 0 {
				n = 0
			}
			if n > info.TopoHeight {
				n = info.TopoHeight
			}
			startTopo = n
		}
	}
	var blocks []explorerBlock
	for topo := startTopo; topo >= 0 && len(blocks) < limit; topo-- {
		var result rpc.GetBlockHeaderByHeight_Result
		if err := a.daemonCall("getblockheaderbytopoheight", rpc.GetBlockHeaderByTopoHeight_Params{TopoHeight: uint64(topo)}, &result); err == nil {
			blocks = append(blocks, a.explorerBlock(result.Block_Header))
		}
		if topo == 0 {
			break
		}
	}
	endTopo := startTopo
	if len(blocks) > 0 {
		endTopo = blocks[len(blocks)-1].TopoHeight
	}
	nextStart := int64(-1)
	if len(blocks) > 0 && endTopo > 0 {
		nextStart = endTopo - 1
	}
	newerStart := int64(-1)
	if startTopo < info.TopoHeight {
		newerStart = startTopo + int64(limit)
		if newerStart > info.TopoHeight {
			newerStart = info.TopoHeight
		}
	}
	writeJSON(w, map[string]interface{}{
		"blocks":     blocks,
		"latestTopo": info.TopoHeight,
		"startTopo":  startTopo,
		"endTopo":    endTopo,
		"nextStart":  nextStart,
		"newerStart": newerStart,
	})
}

func (a *app) handleWalletTransactions(w http.ResponseWriter, r *http.Request) {
	limit := 80
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	a.mu.Lock()
	wallet := a.wallet
	a.mu.Unlock()
	if wallet == nil {
		writeError(w, fmt.Errorf("wallet is not ready"))
		return
	}
	if err := wallet.Sync_Wallet_Memory_With_Daemon(); err != nil {
		writeError(w, err)
		return
	}

	var zeroscid crypto.Hash
	entries := wallet.Show_Transfers(zeroscid, true, true, true, 0, 0, "", "", 0, 0)
	mined := map[string]bool{}
	for _, entry := range entries {
		if entry.TXID != "" {
			mined[entry.TXID] = true
		}
	}

	a.mu.Lock()
	pending := append([]walletTx{}, a.pendingTxs...)
	a.mu.Unlock()

	txs := make([]walletTx, 0, minInt(limit, len(entries)+len(pending)))
	seen := map[string]bool{}
	for i := len(pending) - 1; i >= 0 && len(txs) < limit; i-- {
		if mined[pending[i].TXID] {
			continue
		}
		txs = append(txs, pending[i])
		seen[pending[i].TXID] = true
	}
	for _, entry := range wallet.Show_Mempool_Transfers(zeroscid) {
		if len(txs) >= limit || mined[entry.TXID] || seen[entry.TXID] {
			continue
		}
		tx := walletEntryView(entry)
		tx.Pending = true
		txs = append(txs, tx)
		seen[tx.TXID] = true
	}
	for i := len(entries) - 1; i >= 0 && len(txs) < limit; i-- {
		txs = append(txs, walletEntryView(entries[i]))
	}
	writeJSON(w, map[string]interface{}{"transactions": txs})
}

func (a *app) explorerBlock(header rpc.BlockHeader_Print) explorerBlock {
	view := explorerBlock{
		TopoHeight: header.TopoHeight,
		Height:     header.Height,
		Hash:       header.Hash,
		Timestamp:  header.Timestamp,
		Reward:     rpc.FormatMoney(header.Reward),
		TXCount:    int(header.TXCount),
	}

	var blockResult rpc.GetBlock_Result
	if err := a.daemonCall("getblock", rpc.GetBlock_Params{Height: uint64(header.TopoHeight)}, &blockResult); err != nil {
		view.Transactions = append(view.Transactions, explorerTx{Type: "区块详情读取失败", Note: err.Error()})
		return view
	}

	rawBlock, err := hex.DecodeString(blockResult.Blob)
	if err != nil {
		view.Transactions = append(view.Transactions, explorerTx{Type: "区块解析失败", Note: err.Error()})
		return view
	}
	var bl blockpkg.Block
	if err := bl.Deserialize(rawBlock); err != nil {
		view.Transactions = append(view.Transactions, explorerTx{Type: "区块解析失败", Note: err.Error()})
		return view
	}

	view.MiniBlocks = len(bl.MiniBlocks)
	minerTX := bl.Miner_TX
	view.Transactions = append(view.Transactions, summarizeTransaction(minerTX.GetHash().String(), minerTX.Serialize(), rpc.Tx_Related_Info{
		Block_Height: header.TopoHeight,
		Reward:       header.Reward,
		ValidBlock:   header.Hash,
	}))

	if len(bl.Tx_hashes) == 0 {
		return view
	}

	hashes := make([]string, 0, len(bl.Tx_hashes))
	for _, hash := range bl.Tx_hashes {
		hashes = append(hashes, hash.String())
	}
	var txResult rpc.GetTransaction_Result
	if err := a.daemonCall("gettransactions", rpc.GetTransaction_Params{Tx_Hashes: hashes}, &txResult); err != nil {
		view.Transactions = append(view.Transactions, explorerTx{Type: "交易详情读取失败", Note: err.Error()})
		return view
	}

	for i, hash := range hashes {
		var raw []byte
		if i < len(txResult.Txs_as_hex) && txResult.Txs_as_hex[i] != "" {
			if decoded, err := hex.DecodeString(txResult.Txs_as_hex[i]); err == nil {
				raw = decoded
			}
		}
		related := rpc.Tx_Related_Info{}
		if i < len(txResult.Txs) {
			related = txResult.Txs[i]
		}
		view.Transactions = append(view.Transactions, summarizeTransaction(hash, raw, related))
	}

	return view
}

func summarizeTransaction(hash string, raw []byte, related rpc.Tx_Related_Info) explorerTx {
	view := explorerTx{
		Hash:        hash,
		Size:        len(raw),
		BlockHeight: related.Block_Height,
		ValidBlock:  related.ValidBlock,
		InPool:      related.In_pool,
	}
	if len(raw) == 0 {
		view.Type = "未知"
		view.Note = "交易原始数据不可用"
		return view
	}

	var tx transaction.Transaction
	if err := tx.Deserialize(raw); err != nil {
		view.Type = "解析失败"
		view.Note = err.Error()
		return view
	}

	view.Type = tx.TransactionType.String()
	view.Fees = rpc.FormatMoney(tx.Fees())
	view.Payloads = len(tx.Payloads)
	if len(tx.Payloads) > 0 {
		view.RingSize = tx.Payloads[0].Statement.RingSize
		var burn uint64
		for _, payload := range tx.Payloads {
			burn += payload.BurnValue
		}
		if burn > 0 {
			view.Burn = rpc.FormatMoney(burn)
		}
	}
	if tx.IsCoinbase() {
		view.Type = "COINBASE"
		if related.Reward > 0 {
			view.Note = "矿工奖励 " + rpc.FormatMoney(related.Reward)
		}
	}
	if tx.TransactionType == transaction.SC_TX {
		view.SCID = tx.GetHash().String()
		if tx.SCDATA.Has(rpc.SCACTION, rpc.DataUint64) {
			view.SCAction = fmt.Sprint(tx.SCDATA.Value(rpc.SCACTION, rpc.DataUint64))
		}
		if related.Code != "" {
			view.CodeSize = len(related.Code)
		}
	}
	return view
}

func walletEntryView(entry rpc.Entry) walletTx {
	view := walletTx{
		TXID:       entry.TXID,
		BlockHash:  entry.BlockHash,
		Height:     entry.Height,
		TopoHeight: entry.TopoHeight,
		Time:       entry.Time.Format("2006-01-02 15:04:05"),
		Amount:     rpc.FormatMoney(entry.Amount),
	}
	if entry.TXID == "" {
		view.TXID = entry.BlockHash
	}
	switch {
	case entry.Coinbase:
		view.Kind = "挖矿奖励"
	case entry.Incoming:
		view.Kind = "收到"
		view.Counterparty = entry.Sender
	default:
		view.Kind = "发出"
		view.Counterparty = entry.Destination
		view.Fees = rpc.FormatMoney(entry.Fees)
	}
	args, err := entry.ProcessPayload()
	if err == nil {
		view.PayloadArgs = argumentViews(args)
		if args.Has(rpc.RPC_COMMENT, rpc.DataString) {
			view.Message = args.Value(rpc.RPC_COMMENT, rpc.DataString).(string)
		}
	}
	return view
}

func (a *app) addPendingTransfer(txid, destination string, amount uint64, memo string) {
	view := walletTx{
		Kind:         "发出",
		TXID:         txid,
		Time:         time.Now().Format("2006-01-02 15:04:05"),
		Amount:       rpc.FormatMoney(amount),
		Counterparty: destination,
		Message:      memo,
		Pending:      true,
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.pendingTxs {
		if a.pendingTxs[i].TXID == txid {
			a.pendingTxs[i] = view
			return
		}
	}
	a.pendingTxs = append(a.pendingTxs, view)
	if len(a.pendingTxs) > 100 {
		a.pendingTxs = a.pendingTxs[len(a.pendingTxs)-100:]
	}
}

func argumentViews(args rpc.Arguments) []map[string]string {
	views := make([]map[string]string, 0, len(args))
	for _, arg := range args {
		views = append(views, map[string]string{
			"name":  arg.Name,
			"type":  fmt.Sprint(arg.DataType),
			"value": fmt.Sprint(arg.Value),
		})
	}
	return views
}

type rpcArgument struct {
	Name     string       `json:"name"`
	DataType rpc.DataType `json:"datatype"`
	Value    interface{}  `json:"value"`
}

func (a rpcArgument) convert() (rpc.Argument, error) {
	name := strings.TrimSpace(a.Name)
	if name == "" {
		return rpc.Argument{}, fmt.Errorf("argument name is required")
	}
	switch a.DataType {
	case rpc.DataString:
		return rpc.Argument{Name: name, DataType: rpc.DataString, Value: fmt.Sprint(a.Value)}, nil
	case rpc.DataUint64:
		v, err := parseUintValue(a.Value)
		if err != nil {
			return rpc.Argument{}, fmt.Errorf("%s: %w", name, err)
		}
		return rpc.Argument{Name: name, DataType: rpc.DataUint64, Value: v}, nil
	case rpc.DataInt64:
		v, err := parseIntValue(a.Value)
		if err != nil {
			return rpc.Argument{}, fmt.Errorf("%s: %w", name, err)
		}
		return rpc.Argument{Name: name, DataType: rpc.DataInt64, Value: v}, nil
	case rpc.DataHash:
		return rpc.Argument{Name: name, DataType: rpc.DataHash, Value: crypto.HashHexToHash(fmt.Sprint(a.Value))}, nil
	default:
		return rpc.Argument{}, fmt.Errorf("unsupported argument datatype %q", a.DataType)
	}
}

func (a *app) daemonCall(method string, params interface{}, result interface{}) error {
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "1",
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Post("http://"+a.daemonRPC+"/json_rpc", "application/json", bytes.NewReader(data))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("daemon rpc status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var decoded struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return err
	}
	if decoded.Error != nil {
		return fmt.Errorf("daemon rpc error %d: %s", decoded.Error.Code, decoded.Error.Message)
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(decoded.Result, result)
}

func (a *app) ensureSpendableWallet(wallet *walletapi.Wallet_Disk) error {
	if wallet == nil {
		return fmt.Errorf("钱包还没有准备好，请稍后重试")
	}

	var info rpc.GetInfo_Result
	if err := a.daemonCall("get_info", nil, &info); err != nil {
		return fmt.Errorf("节点还没有准备好，无法确认钱包注册状态: %w", err)
	}

	_ = wallet.Sync_Wallet_Memory_With_Daemon()
	self := wallet.GetAddress().String()
	latest, latestErr := a.encryptedBalance(self, -1)
	if wallet.IsRegistered() || (latestErr == nil && latest.Status == "OK") {
		registrationTopo := wallet.Get_Registration_TopoHeight()
		if latestErr == nil && latest.Status == "OK" && latest.Registration >= 0 {
			registrationTopo = latest.Registration
		}
		remaining := registrationConfirmationsRemaining(info.TopoHeight, registrationTopo)
		if remaining > 0 {
			return walletRegistrationConfirmingError("当前钱包", remaining, info.TopoHeight, registrationTopo)
		}
		if !wallet.IsRegistered() {
			_ = wallet.Sync_Wallet_Memory_With_Daemon()
			if !wallet.IsRegistered() {
				return fmt.Errorf("当前钱包已在节点上注册，但本地钱包还没有同步到注册状态；请等待几秒或点击刷新后重试（当前 topoheight=%d，注册 topoheight=%d）", info.TopoHeight, registrationTopo)
			}
		}
		return nil
	}

	a.mu.Lock()
	reg := a.registration
	a.mu.Unlock()
	if reg.Running {
		return fmt.Errorf("当前钱包正在生成并提交注册交易，请等待注册交易进入区块")
	}
	if reg.Done && reg.TXID != "" {
		return fmt.Errorf("当前钱包注册交易已提交，但还没有被新区块确认；请开始挖矿或等待其他矿工出块。确认后才能转账或部署合约。注册交易 TXID: %s", reg.TXID)
	}
	if latestErr != nil && !strings.Contains(latestErr.Error(), "Account Unregistered") {
		return fmt.Errorf("无法确认当前钱包注册状态: %w", latestErr)
	}
	return fmt.Errorf("当前钱包还没有上链注册；请点击“开始挖矿”完成首次注册，注册交易进块并确认后才能转账或部署合约")
}

func (a *app) preflightTransfer(wallet *walletapi.Wallet_Disk, destination string) error {
	if destination == "" {
		return fmt.Errorf("收款地址不能为空")
	}
	dest, err := globals.ParseValidateAddress(destination)
	if err != nil {
		return fmt.Errorf("收款地址无效: %w", err)
	}

	var info rpc.GetInfo_Result
	if err := a.daemonCall("get_info", nil, &info); err != nil {
		return fmt.Errorf("节点还没有准备好，无法转账: %w", err)
	}

	var zeroscid crypto.Hash
	self := wallet.GetAddress().String()
	_, nonceTopo, _, _, err := wallet.GetEncryptedBalanceAtTopoHeight(zeroscid, -1, self)
	if err != nil {
		if latest, latestErr := a.encryptedBalance(self, -1); latestErr == nil {
			return explainBalanceLookupError("当前钱包", self, info.TopoHeight-registrationConfirmations, info.TopoHeight, err, latest)
		}
		a.mu.Lock()
		reg := a.registration
		a.mu.Unlock()
		if reg.Done && reg.TXID != "" {
			return fmt.Errorf("当前钱包注册交易已提交，但还没有被新区块确认；请开始挖矿或等待其他矿工出块。注册交易 TXID: %s", reg.TXID)
		}
		return fmt.Errorf("当前钱包还没有上链注册；请点击“开始挖矿”完成首次注册，注册交易进块并确认后才能转账")
	}

	checkTopo := int64(-1)
	if info.TopoHeight >= int64(nonceTopo)+3 {
		checkTopo = info.TopoHeight - 3
	}

	if _, err := a.encryptedBalance(self, checkTopo); err != nil {
		return explainBalanceLookupError("当前钱包", self, checkTopo, info.TopoHeight, err, rpc.GetEncryptedBalance_Result{})
	}

	destAddress := dest.BaseAddress().String()
	if result, err := a.encryptedBalance(destAddress, checkTopo); err != nil {
		latest, latestErr := a.encryptedBalance(destAddress, -1)
		if latestErr == nil {
			return explainBalanceLookupError("收款地址", destAddress, checkTopo, info.TopoHeight, err, latest)
		}
		return fmt.Errorf("收款地址还没有上链注册，请让对方先打开客户端并挖矿完成一次链上注册")
	} else if checkTopo >= 0 && result.Registration > checkTopo {
		return explainBalanceLookupError("收款地址", destAddress, checkTopo, info.TopoHeight, errAccountWaitingForConfirmations{}, result)
	}

	return nil
}

func (a *app) encryptedBalance(address string, topoheight int64) (rpc.GetEncryptedBalance_Result, error) {
	var result rpc.GetEncryptedBalance_Result
	var zeroscid crypto.Hash
	err := a.daemonCall("DERO.GetEncryptedBalance", rpc.GetEncryptedBalance_Params{
		Address:    address,
		SCID:       zeroscid,
		TopoHeight: topoheight,
	}, &result)
	return result, err
}

func registrationConfirmationsRemaining(currentTopo, registrationTopo int64) int64 {
	if registrationTopo < 0 {
		return 0
	}
	checkTopo := currentTopo - registrationConfirmations
	if registrationTopo <= checkTopo {
		return 0
	}
	return registrationTopo - checkTopo
}

func minerAddressMaturityRemaining(currentTopo, registrationTopo int64) int64 {
	if registrationTopo < 0 {
		return 0
	}
	waitUntil := registrationTopo + minerAddressMaturity
	if currentTopo >= waitUntil {
		return 0
	}
	return waitUntil - currentTopo
}

type errAccountWaitingForConfirmations struct{}

func (errAccountWaitingForConfirmations) Error() string {
	return "account is waiting for confirmations"
}

func explainBalanceLookupError(label, address string, checkTopo, currentTopo int64, err error, latest rpc.GetEncryptedBalance_Result) error {
	if latest.Status == "OK" && checkTopo >= 0 && latest.Registration > checkTopo {
		return walletRegistrationConfirmingError(label, latest.Registration-checkTopo, currentTopo, latest.Registration)
	}
	if strings.Contains(err.Error(), "Account Unregistered") {
		return fmt.Errorf("%s还没有上链注册：%s", label, address)
	}
	return fmt.Errorf("无法读取%s的链上余额状态: %w", label, err)
}

func walletRegistrationConfirmingError(label string, remaining, currentTopo, registrationTopo int64) error {
	if remaining < 1 {
		remaining = 1
	}
	return fmt.Errorf("%s已注册，但注册区块确认数还不够；还需要大约 %d 个区块确认后再重试（当前 topoheight=%d，注册 topoheight=%d）", label, remaining, currentTopo, registrationTopo)
}

func parseMoney(input string) (uint64, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return 0, fmt.Errorf("amount is required")
	}
	if strings.HasPrefix(s, "-") {
		return 0, fmt.Errorf("amount cannot be negative")
	}
	parts := strings.SplitN(s, ".", 2)
	whole, err := strconv.ParseUint(parts[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount")
	}
	frac := ""
	if len(parts) == 2 {
		frac = parts[1]
	}
	if len(frac) > 5 {
		return 0, fmt.Errorf("DERO supports up to 5 decimal places here")
	}
	for len(frac) < 5 {
		frac += "0"
	}
	fracValue := uint64(0)
	if frac != "" {
		fracValue, err = strconv.ParseUint(frac, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid amount")
		}
	}
	if whole > math.MaxUint64/100000 {
		return 0, fmt.Errorf("amount is too large")
	}
	return whole*100000 + fracValue, nil
}

func parseUintValue(value interface{}) (uint64, error) {
	switch v := value.(type) {
	case float64:
		if v < 0 || v != math.Trunc(v) {
			return 0, fmt.Errorf("expected uint64")
		}
		return uint64(v), nil
	case string:
		return strconv.ParseUint(strings.TrimSpace(v), 10, 64)
	default:
		return 0, fmt.Errorf("expected uint64")
	}
}

func parseIntValue(value interface{}) (int64, error) {
	switch v := value.(type) {
	case float64:
		if v != math.Trunc(v) {
			return 0, fmt.Errorf("expected int64")
		}
		return int64(v), nil
	case string:
		return strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	default:
		return 0, fmt.Errorf("expected int64")
	}
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "error": err.Error()})
}

func processTarget(proc *managedProcess) string {
	if proc == nil {
		return ""
	}
	return proc.target
}

func findBundledBinary(name string) (string, error) {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "bin", exeName(name)),
			filepath.Join(base, exeName(name)),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "build", "catfish-linux", exeName(name)),
			filepath.Join(cwd, "build", "catfish-windows", exeName(name)),
			filepath.Join(cwd, "bin", exeName(name)),
		)
	}
	if path, err := exec.LookPath(exeName(name)); err == nil {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("%s was not found", exeName(name))
}

func copyIfNeeded(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if dstInfo, err := os.Stat(dst); err == nil && dstInfo.Size() == srcInfo.Size() && dstInfo.ModTime().After(srcInfo.ModTime().Add(-time.Second)) {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0755); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

func exeName(name string) string {
	if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(name), ".exe") {
		return name + ".exe"
	}
	return name
}

func resolveDataDir(flagValue string) (string, error) {
	if flagValue != "" {
		return filepath.Abs(flagValue)
	}
	if runtime.GOOS == "windows" {
		if local := os.Getenv("LOCALAPPDATA"); local != "" {
			return filepath.Join(local, "CatfishDeroPublic"), nil
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "catfish-dero-public"), nil
}

func envDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func waitHTTP(port string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	url := "http://127.0.0.1:" + port + "/api/desktop"
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("local UI did not start: %s", url)
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	fmt.Println("Press Enter to exit.")
	_, _ = fmt.Scanln()
	os.Exit(1)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
