//go:build windows

package main

import (
	"errors"
	"os/exec"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	pOpenClipboard   = user32.NewProc("OpenClipboard")
	pCloseClipboard  = user32.NewProc("CloseClipboard")
	pEmptyClipboard  = user32.NewProc("EmptyClipboard")
	pSetClipboard    = user32.NewProc("SetClipboardData")
	pMessageBeep     = user32.NewProc("MessageBeep")
	pGlobalAlloc     = kernel32.NewProc("GlobalAlloc")
	pGlobalFree      = kernel32.NewProc("GlobalFree")
	pGlobalLock      = kernel32.NewProc("GlobalLock")
	pGlobalUnlock    = kernel32.NewProc("GlobalUnlock")
	pRtlMoveMemory   = kernel32.NewProc("RtlMoveMemory")
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

// copiarTexto coloca o texto na área de transferência do Windows (Ctrl+V).
func copiarTexto(texto string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var ok uintptr
	for i := 0; i < 20; i++ {
		ok, _, _ = pOpenClipboard.Call(0)
		if ok != 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if ok == 0 {
		return errors.New("área de transferência ocupada por outro programa")
	}
	defer pCloseClipboard.Call()
	pEmptyClipboard.Call()

	u, err := syscall.UTF16FromString(texto)
	if err != nil {
		return err
	}
	tamanho := uintptr(len(u) * 2)
	h, _, _ := pGlobalAlloc.Call(gmemMoveable, tamanho)
	if h == 0 {
		return errors.New("sem memória para copiar")
	}
	p, _, _ := pGlobalLock.Call(h)
	if p == 0 {
		pGlobalFree.Call(h)
		return errors.New("falha ao travar memória")
	}
	pRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&u[0])), tamanho)
	pGlobalUnlock.Call(h)
	if r, _, _ := pSetClipboard.Call(cfUnicodeText, h); r == 0 {
		pGlobalFree.Call(h)
		return errors.New("o Windows recusou a cópia")
	}
	return nil
}

func apitar() { pMessageBeep.Call(0x00000040) } // som de "informação" do Windows

func abrirNavegador(url string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}
