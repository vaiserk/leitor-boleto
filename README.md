# Leitor de Boletos

Página do celular que lê o código de barras de boletos com a câmera, confere os dígitos verificadores e envia o código, criptografado (AES-256-GCM), para o **Receptor de Boletos** no PC, que copia a linha digitável para a área de transferência.

- Funciona com celular e PC em redes diferentes (as mensagens passam pelo [ntfy.sh](https://ntfy.sh) já criptografadas).
- O pareamento é feito lendo o QR code mostrado pelo Receptor no PC; a chave fica no endereço depois do `#`, que não é enviado a nenhum servidor.
- Não acessa nem altera o site do banco: só copia o número.

Publicado via GitHub Pages: `https://vaiserk.github.io/leitor-boleto/`
