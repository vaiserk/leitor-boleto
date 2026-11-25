package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Regras de validação do boleto (FEBRABAN) — iguais às da página do celular.

func soDigitos(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mod10(n string) int {
	soma, peso := 0, 2
	for i := len(n) - 1; i >= 0; i-- {
		p := int(n[i]-'0') * peso
		if p > 9 {
			p = p/10 + p%10
		}
		soma += p
		if peso == 2 {
			peso = 1
		} else {
			peso = 2
		}
	}
	return (10 - soma%10) % 10
}

func somaMod11(n string) int {
	soma, peso := 0, 2
	for i := len(n) - 1; i >= 0; i-- {
		soma += int(n[i]-'0') * peso
		if peso == 9 {
			peso = 2
		} else {
			peso++
		}
	}
	return soma % 11
}

func mod11Banco(n string) int {
	dv := 11 - somaMod11(n)
	if dv == 0 || dv == 10 || dv == 11 {
		return 1
	}
	return dv
}

func mod11Arrecadacao(n string) int {
	r := somaMod11(n)
	if r == 0 || r == 1 {
		return 0
	}
	return 11 - r
}

type Boleto struct {
	Tipo   string   `json:"tipo"`
	Codigo string   `json:"codigo"`
	Linha  string   `json:"linha"`
	Valor  *float64 `json:"valor"`
	Venc   *string  `json:"venc"`
}

// Analisar recebe os 44 dígitos do código de barras e devolve nil se não for válido.
func Analisar(codigo string) *Boleto {
	cod := soDigitos(codigo)
	if len(cod) != 44 {
		return nil
	}
	dig := func(i int) int { return int(cod[i] - '0') }

	if cod[0] == '8' { // conta de consumo, tributo, convênio
		idValor := cod[2]
		if !strings.ContainsRune("6789", rune(idValor)) {
			return nil
		}
		calc := mod11Arrecadacao
		if idValor == '6' || idValor == '7' {
			calc = mod10
		}
		if calc(cod[:3]+cod[4:]) != dig(3) {
			return nil
		}
		var linha strings.Builder
		for i := 0; i < 4; i++ {
			b := cod[i*11 : i*11+11]
			linha.WriteString(b + strconv.Itoa(calc(b)))
		}
		var valor *float64
		if idValor == '6' || idValor == '8' {
			v, _ := strconv.Atoi(cod[4:15])
			f := float64(v) / 100
			valor = &f
		}
		return &Boleto{Tipo: "Convênio / tributo", Codigo: cod, Linha: linha.String(), Valor: valor}
	}

	// boleto bancário
	if mod11Banco(cod[:4]+cod[5:]) != dig(4) {
		return nil
	}
	livre := cod[19:]
	c1, c2, c3 := cod[:4]+livre[:5], livre[5:15], livre[15:25]
	linha := c1 + strconv.Itoa(mod10(c1)) + c2 + strconv.Itoa(mod10(c2)) + c3 + strconv.Itoa(mod10(c3)) + cod[4:5] + cod[5:19]
	fator, _ := strconv.Atoi(cod[5:9])
	var venc *string
	if fator >= 1000 { // ciclo atual: fator 1000 = 22/02/2025
		d := time.Date(2025, 2, 22, 0, 0, 0, 0, time.UTC).AddDate(0, 0, fator-1000).Format("2006-01-02")
		venc = &d
	}
	v, _ := strconv.Atoi(cod[9:19])
	f := float64(v) / 100
	return &Boleto{Tipo: "Boleto bancário", Codigo: cod, Linha: linha, Valor: &f, Venc: venc}
}

func BRL(v *float64) string {
	if v == nil {
		return "valor no boleto"
	}
	s := fmt.Sprintf("%.2f", *v)
	inteiro, dec := s[:len(s)-3], s[len(s)-2:]
	var b strings.Builder
	for i, r := range inteiro {
		if i > 0 && (len(inteiro)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(r)
	}
	return "R$ " + b.String() + "," + dec
}
