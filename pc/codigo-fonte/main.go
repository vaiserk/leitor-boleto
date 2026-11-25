// Receptor de Boletos — recebe os códigos lidos no celular (Leitor de Boletos)
// e copia a linha digitável para a área de transferência do Windows.
// Não mexe no navegador nem no site do banco: só copia o número (Ctrl+V).
package main

import (
	"bufio"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	enderecoLocal = "127.0.0.1:47123"
	paginaPadrao  = "https://vaiserk.github.io/leitor-boleto/"
)

//go:embed ui.html qrcode.js
var arquivos embed.FS

var servidorNtfy = func() string {
	if s := os.Getenv("LEITOR_BOLETO_SERVIDOR"); s != "" { // usado só em teste
		return s
	}
	return "https://ntfy.sh"
}()

type Config struct {
	Topico   string `json:"topico"`
	Senha    string `json:"senha"`
	Pagina   string `json:"pagina"`
	UltimoID string `json:"ultimo_id"`
	AutoCopy bool   `json:"auto_copiar"`
}

type Item struct {
	Boleto
	ID       string `json:"id"`
	Recebido string `json:"recebido"`
	Pago     bool   `json:"pago"`
}

type App struct {
	mu        sync.Mutex
	pasta     string
	cfg       Config
	chave     []byte
	itens     []Item
	status    string
	token     string
	novidades int // contador para a tela saber que chegou boleto novo
}

func novoTexto(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return strings.TrimRight(base64.RawURLEncoding.EncodeToString(b), "=")
}

func (a *App) caminho(nome string) string { return filepath.Join(a.pasta, nome) }

func lerJSON(caminho string, destino any) bool {
	b, err := os.ReadFile(caminho)
	return err == nil && json.Unmarshal(b, destino) == nil
}

func gravarJSON(caminho string, dados any) {
	b, _ := json.MarshalIndent(dados, "", "  ")
	tmp := caminho + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err == nil {
		_ = os.Rename(tmp, caminho)
	}
}

func derivarChave(senha, topico string) []byte {
	k, err := pbkdf2.Key(sha256.New, senha, []byte("leitor-boleto:"+topico), 200000, 32)
	if err != nil {
		panic(err)
	}
	return k
}

func decifrar(chave []byte, texto string) (map[string]any, error) {
	bruto, err := base64.StdEncoding.DecodeString(strings.TrimSpace(texto))
	if err != nil || len(bruto) < 12+16 {
		return nil, errors.New("formato inválido")
	}
	bloco, _ := aes.NewCipher(chave)
	gcm, _ := cipher.NewGCM(bloco)
	claro, err := gcm.Open(nil, bruto[:12], bruto[12:], nil)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	return m, json.Unmarshal(claro, &m)
}

func (a *App) carregar() {
	if !lerJSON(a.caminho("config.json"), &a.cfg) || a.cfg.Topico == "" || a.cfg.Senha == "" {
		a.cfg = Config{
			Topico:   "boleto-" + strings.NewReplacer("-", "x", "_", "y").Replace(novoTexto(18)),
			Senha:    novoTexto(24),
			Pagina:   paginaPadrao,
			AutoCopy: true,
		}
		gravarJSON(a.caminho("config.json"), a.cfg)
	}
	if a.cfg.Pagina == "" {
		a.cfg.Pagina = paginaPadrao
	}
	lerJSON(a.caminho("historico.json"), &a.itens)
	if a.itens == nil {
		a.itens = []Item{}
	}
	a.chave = derivarChave(a.cfg.Senha, a.cfg.Topico)
	a.status = "Conectando..."
}

func (a *App) setStatus(s string) { a.mu.Lock(); a.status = s; a.mu.Unlock() }

// ---------- recebimento (fica conectado ao servidor de mensagens) ----------

func (a *App) escutar() {
	cliente := &http.Client{Timeout: 0}
	for {
		a.mu.Lock()
		desde := a.cfg.UltimoID
		topico := a.cfg.Topico
		a.mu.Unlock()
		if desde == "" {
			desde = "12h"
		}
		err := a.conectar(cliente, topico, desde)
		a.setStatus(fmt.Sprintf("Sem conexão com a internet (%v). Tentando de novo em 5 s...", err))
		time.Sleep(5 * time.Second)
	}
}

func (a *App) conectar(cliente *http.Client, topico, desde string) error {
	ctx, cancelar := context.WithCancel(context.Background())
	defer cancelar()
	req, _ := http.NewRequestWithContext(ctx, "GET", servidorNtfy+"/"+topico+"/json?since="+url.QueryEscape(desde), nil)
	resp, err := cliente.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	a.setStatus("Conectado — aguardando boletos do celular.")

	// se nada chegar em 2 min (nem o "keepalive"), derruba e reconecta
	vigia := time.AfterFunc(2*time.Minute, cancelar)
	defer vigia.Stop()

	leitor := bufio.NewScanner(resp.Body)
	leitor.Buffer(make([]byte, 64*1024), 1024*1024)
	for leitor.Scan() {
		vigia.Reset(2 * time.Minute)
		var ev struct {
			ID      string `json:"id"`
			Event   string `json:"event"`
			Message string `json:"message"`
		}
		if json.Unmarshal(leitor.Bytes(), &ev) != nil || ev.Event != "message" {
			continue
		}
		a.receber(ev.ID, ev.Message)
	}
	if err := leitor.Err(); err != nil {
		return err
	}
	return io.ErrUnexpectedEOF
}

func (a *App) receber(id, mensagem string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.cfg.UltimoID = id
	gravarJSON(a.caminho("config.json"), a.cfg)
	for _, it := range a.itens {
		if it.ID == id {
			return
		}
	}
	dados, err := decifrar(a.chave, mensagem)
	if err != nil {
		return // não veio do seu celular: ignora
	}
	codigo, _ := dados["codigo"].(string)
	b := Analisar(codigo) // confere os dígitos de novo aqui no PC
	if b == nil {
		a.status = "Chegou um código inválido e ele foi ignorado."
		return
	}
	repetido := false
	for _, it := range a.itens {
		if it.Codigo == b.Codigo {
			repetido = true
		}
	}
	a.itens = append(a.itens, Item{Boleto: *b, ID: id, Recebido: time.Now().Format("02/01 15:04")})
	gravarJSON(a.caminho("historico.json"), a.itens)
	a.novidades++

	aviso := ""
	if repetido {
		aviso = " ATENÇÃO: esse boleto já tinha chegado antes."
	}
	if a.cfg.AutoCopy {
		if err := copiarTexto(b.Linha); err != nil {
			a.status = fmt.Sprintf("Recebido %s, mas não consegui copiar (%v). Use o botão Copiar.%s", BRL(b.Valor), err, aviso)
		} else {
			a.status = fmt.Sprintf("Recebido %s — linha digitável copiada. É só dar Ctrl+V no banco.%s", BRL(b.Valor), aviso)
		}
	} else {
		a.status = fmt.Sprintf("Recebido %s.%s", BRL(b.Valor), aviso)
	}
	go apitar()
}

// ---------- tela (servida só para este computador) ----------

func (a *App) rotas() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		b, _ := arquivos.ReadFile("ui.html")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(b)
	})
	mux.HandleFunc("GET /qrcode.js", func(w http.ResponseWriter, r *http.Request) {
		b, _ := arquivos.ReadFile("qrcode.js")
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Write(b)
	})

	api := func(f func(w http.ResponseWriter, r *http.Request)) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Token") != a.token {
				http.Error(w, "acesso negado", http.StatusForbidden)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			f(w, r)
		}
	}
	lerCorpo := func(r *http.Request, v any) { _ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v) }

	mux.HandleFunc("GET /api/estado", api(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		itens := a.itens
		if itens == nil {
			itens = []Item{}
		}
		json.NewEncoder(w).Encode(map[string]any{
			"status": a.status, "auto": a.cfg.AutoCopy, "itens": itens, "novidades": a.novidades,
			"pagina": a.cfg.Pagina,
			"pareamento": a.cfg.Pagina + "#t=" + a.cfg.Topico + "&k=" + a.cfg.Senha,
		})
	}))

	mux.HandleFunc("POST /api/copiar", api(func(w http.ResponseWriter, r *http.Request) {
		var p struct{ ID, Campo string }
		lerCorpo(r, &p)
		a.mu.Lock()
		defer a.mu.Unlock()
		for _, it := range a.itens {
			if it.ID == p.ID {
				texto, nome := it.Linha, "Linha digitável"
				if p.Campo == "codigo" {
					texto, nome = it.Codigo, "Código de barras"
				}
				if err := copiarTexto(texto); err != nil {
					a.status = "Não consegui copiar: " + err.Error()
				} else {
					a.status = fmt.Sprintf("%s copiado (%s). Ctrl+V no banco.", nome, BRL(it.Valor))
				}
				break
			}
		}
		w.Write([]byte(`{}`))
	}))

	mux.HandleFunc("POST /api/pago", api(func(w http.ResponseWriter, r *http.Request) {
		var p struct{ ID string }
		lerCorpo(r, &p)
		a.mu.Lock()
		defer a.mu.Unlock()
		for i := range a.itens {
			if a.itens[i].ID == p.ID {
				a.itens[i].Pago = !a.itens[i].Pago
			}
		}
		gravarJSON(a.caminho("historico.json"), a.itens)
		w.Write([]byte(`{}`))
	}))

	mux.HandleFunc("POST /api/remover", api(func(w http.ResponseWriter, r *http.Request) {
		var p struct{ ID string }
		lerCorpo(r, &p)
		a.mu.Lock()
		defer a.mu.Unlock()
		novos := a.itens[:0]
		for _, it := range a.itens {
			if it.ID != p.ID {
				novos = append(novos, it)
			}
		}
		a.itens = novos
		gravarJSON(a.caminho("historico.json"), a.itens)
		w.Write([]byte(`{}`))
	}))

	mux.HandleFunc("POST /api/config", api(func(w http.ResponseWriter, r *http.Request) {
		var p struct {
			Auto   *bool
			Pagina *string
		}
		lerCorpo(r, &p)
		a.mu.Lock()
		defer a.mu.Unlock()
		if p.Auto != nil {
			a.cfg.AutoCopy = *p.Auto
		}
		if p.Pagina != nil {
			if u, err := url.Parse(strings.TrimSpace(*p.Pagina)); err == nil && u.Scheme == "https" && u.Host != "" {
				s := u.String()
				if !strings.HasSuffix(s, "/") && !strings.HasSuffix(s, ".html") {
					s += "/"
				}
				a.cfg.Pagina = s
			}
		}
		gravarJSON(a.caminho("config.json"), a.cfg)
		w.Write([]byte(`{}`))
	}))

	mux.HandleFunc("POST /api/sair", api(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{}`))
		go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
	}))

	// só aceita acesso pelo próprio computador (bloqueia truques de outros sites)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != enderecoLocal && r.Host != strings.Replace(enderecoLocal, "127.0.0.1", "localhost", 1) {
			http.Error(w, "acesso negado", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func main() {
	base, err := os.UserConfigDir()
	if err != nil {
		base = "."
	}
	app := &App{pasta: filepath.Join(base, "LeitorBoleto")}
	_ = os.MkdirAll(app.pasta, 0o700)

	if f, err := os.OpenFile(app.caminho("log.txt"), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err == nil {
		log.SetOutput(io.MultiWriter(f, os.Stderr))
	}

	ouvinte, err := net.Listen("tcp", enderecoLocal)
	if err != nil {
		// já está aberto: só mostra a tela de novo
		if t, e := os.ReadFile(app.caminho("sessao.txt")); e == nil {
			abrirNavegador("http://" + enderecoLocal + "/#" + strings.TrimSpace(string(t)))
		}
		return
	}

	app.carregar()
	tb := make([]byte, 16)
	_, _ = rand.Read(tb)
	app.token = hex.EncodeToString(tb)
	_ = os.WriteFile(app.caminho("sessao.txt"), []byte(app.token), 0o600)

	go app.escutar()
	go func() {
		time.Sleep(300 * time.Millisecond)
		abrirNavegador("http://" + enderecoLocal + "/#" + app.token)
	}()
	log.Println("Receptor de Boletos rodando em http://" + enderecoLocal)
	log.Fatal(http.Serve(ouvinte, app.rotas()))
}
