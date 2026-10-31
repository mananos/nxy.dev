---
name: security
title: Security-sensitive code
paths: **/auth/**, **/*auth*, **/security/**, **/*security*, **/*payment*, **/payments/**, **/*webhook*, **/*token*, **/*crypto*, **/*password*, **/*permission*
content: password, secret, apiKey, api_key, jwt, BCrypt, MessageDigest, Cipher, @PreAuthorize, @Secured, innerHTML, dangerouslySetInnerHTML, eval(, Runtime.getRuntime
---
- **Authorization** checked on the server for every path the change opens, not only hidden in the UI.
- **Secrets**: none in code, config committed to the repo, logs or error messages.
- **Injection**: queries parameterised; no user input in shell commands, `eval`, HTML (`innerHTML`, `bypassSecurityTrust…`) or file paths without validation.
- **Crypto and tokens**: standard libraries, no homemade hashing; tokens with expiry and signature verification (webhooks included).
- **Personal data** not logged and not exposed in responses beyond what the client needs.
