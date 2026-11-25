//go:build !windows

package main

import "log"

// Versões de teste para rodar fora do Windows.
func copiarTexto(texto string) error { log.Println("[área de transferência]", texto); return nil }
func apitar()                        { log.Println("[bip]") }
func abrirNavegador(url string)      { log.Println("[abrir navegador]", url) }
