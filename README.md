# Leitor de Boletos

Página do celular que lê o código de barras de boletos com a câmera, confere os dígitos verificadores e envia o código, criptografado (AES-256-GCM), para o **Receptor de Boletos** no PC, que copia a linha digitável para a área de transferência.

- Funciona com celular e PC em redes diferentes (as mensagens passam pelo [ntfy.sh](https://ntfy.sh) já criptografadas).
- O pareamento é feito lendo o QR code mostrado pelo Receptor no PC; a chave fica no endereço depois do `#`, que não é enviado a nenhum servidor.
- Não acessa nem altera o site do banco: só copia o número.

Publicado via GitHub Pages: `https://vaiserk.github.io/leitor-boleto/`

## Programa do PC (Windows)

Baixe [`pc/receptor-boletos.exe`](pc/receptor-boletos.exe) e dê dois cliques — não precisa instalar nada.

- Na primeira vez o Windows pode mostrar "O Windows protegeu o computador" (o programa não tem assinatura digital): clique em **Mais informações → Executar assim mesmo**.
- Ele abre uma tela no navegador com a lista de boletos e o QR code para parear o celular.
- Para fechar, use o botão **Encerrar programa** na tela.

O código-fonte está em [`pc/codigo-fonte`](pc/codigo-fonte) (Go, só biblioteca padrão). Para compilar de novo:

```
cd pc/codigo-fonte
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-H=windowsgui -s -w" -o ../receptor-boletos.exe .
```

`qrcode.js` é a biblioteca [qrcode-generator](https://github.com/kazuhikoarase/qrcode-generator) (licença MIT).
