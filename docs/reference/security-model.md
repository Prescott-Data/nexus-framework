# Security Model

The Nexus Framework is architected around a central security thesis: **The Principle of Least Privilege for AI Agents**.

In a traditional system, an application holds the long-term "Refresh Token" or "API Key" directly in its memory or environment variables. If that application (the Agent) is compromised via Remote Code Execution (RCE) or a dependency vulnerability, the attacker gains permanent access to the user's account.

Nexus eliminates this risk by decoupling the **Custody of Credentials** from the **Usage of Credentials**.

---

## 1. The Core Philosophy: Secret Separation

Nexus defines two distinct classes of secrets. Understanding this distinction is critical to understanding the framework's value.

### Type A: Master Secrets (Custody)
- **Definition:** Long-lived credentials that grant persistent access to a resource.
- **Examples:** OAuth 2.0 Refresh Tokens, Static API Keys, AWS Root Keys.
- **Location:** Encrypted at rest in the **Nexus Broker's** database. Never leaves the Broker's boundary except during internal processing.
- **Risk Profile:** High. Compromise leads to potential account takeover.

### Type B: Usage Secrets (Operation)
- **Definition:** Short-lived, scoped credentials used to perform specific actions.
- **Examples:** OAuth 2.0 Access Tokens (1 hour expiry), AWS STS Session Tokens.
- **Location:** Held in the **Agent's memory** (via the Nexus Bridge) only when needed.
- **Risk Profile:** Low. Compromise leads to limited, temporary access that expires automatically.

### The Flow of Trust
1.  **Request:** The Agent (via Bridge) asks the Gateway for credentials to perform a task.
2.  **Verification:** The Gateway authenticates the Agent.
3.  **Decryption:** The Broker retrieves and decrypts the **Master Secret**.
4.  **Exchange:** The Broker sends the Master Secret to the Provider (e.g., Google) to get a fresh **Usage Secret**.
5.  **Delivery:** The Broker returns *only* the **Usage Secret** to the Agent.

**Result:** If the Agent is hacked, the attacker finds only a short-lived token that will expire in minutes. They never find the Refresh Token.

---

## 2. Cryptographic Infrastructure

Nexus relies on three critical keys for its operation. These keys must be generated securely and injected as environment variables.

### A. The Encryption Key (`ENCRYPTION_KEY`)
- **Format:** 32-byte (256-bit) string, Base64 encoded.
- **Algorithm:** AES-GCM (Galois/Counter Mode).
- **Purpose:** Encrypts sensitive data (Master Secrets) before writing to the PostgreSQL database.
- **Generation:** `openssl rand -base64 32`
- **Critical Warning:** If this key is lost, **all stored connections become permanently unrecoverable.** Users would need to re-authenticate every integration.

### B. The State Key (`STATE_KEY`)
- **Format:** 32-byte (256-bit) string, Base64 encoded.
- **Purpose:** Used to sign and verify the integrity of the OIDC `state` parameter and internal tokens.
- **Defense:** Prevents Cross-Site Request Forgery (CSRF) and Replay Attacks during the user consent handshake. It ensures that the user returning from Google is the same user who initiated the flow.
- **Scope:** Shared between the Gateway and Broker to validate internal trust.

### C. The Internal API Key (`API_KEY` / `BROKER_API_KEY`)
- **Format:** High-entropy string (UUID or random alphanumeric).
- **Purpose:** Authenticates the Gateway to the Broker.
- **Defense:** Since the Broker holds the keys to the kingdom, it must accept commands *only* from the trusted Gateway or an Admin. This key is the primary authorization mechanism for the `control plane` (registering providers, refreshing tokens).

---

## 3. Network Hardening & Transport Security

Security is not just about encryption at rest; it is about protecting the path.

### IP Allowlisting (`ALLOWED_CIDRS`)
The Broker includes a middleware that restricts access based on the source IP address.
- **Production Recommendation:** Set `ALLOWED_CIDRS` to the specific internal IP address or subnet of your **Nexus Gateway**.
- **Effect:** Even if the `API_KEY` is leaked, an attacker cannot call the Broker API from the public internet.

### Encryption in Transit
- **External Traffic:** All public endpoints (Gateway) must be served over HTTPS.
- **Internal Traffic:** Communication between the Gateway and Broker should be encrypted.
    - **Platform TLS:** Use your cloud provider's native internal TLS (e.g., AWS App Mesh, Google Cloud Run) if available.
    - **Native TLS:** Configure the Broker to serve HTTPS using a private CA or self-signed certificate if running on bare metal.

---

## 4. Threat Model: What We Protect Against

| Threat Scenario | Nexus Protection |
| :--- | :--- |
| **Rogue Agent:** An attacker gains RCE on your AI Agent container. | **Mitigated.** The attacker can dump the process memory, but they will only find a short-lived Access Token. They cannot steal the Refresh Token to maintain persistence. |
| **Database Leak:** An attacker dumps the Broker's PostgreSQL database. | **Mitigated.** All tokens are encrypted with AES-256-GCM using the `ENCRYPTION_KEY`. Without the key (which lives in the app env, not the DB), the data is useless. |
| **CSRF / Replay:** An attacker tries to trick a user into linking their Google account to the attacker's workspace. | **Prevented.** The `STATE_KEY` ensures that the `state` parameter is cryptographically signed. If the signature doesn't match, the callback is rejected. |
| **Stale Token Reuse:** An attacker finds an old token in a log file. | **Mitigated.** Nexus automatically rotates Access Tokens. Logged tokens are likely already expired. (Note: Nexus SDK attempts to redact tokens from logs by default). |

### Out of Scope (What You Must Secure)
- **The Broker Environment:** If an attacker gains RCE on the **Broker container itself**, they can read the `ENCRYPTION_KEY` from memory and decrypt the database. You must harden the Broker's environment (use private subnets, minimal base images).
- **The Application User Database:** Nexus handles *connections*, not *user authentication*. You are responsible for ensuring that only authorized users can call `POST /v1/request-connection`.
