# OBO Delegation

On Behalf Of (OBO) delegation is a session pattern for multi-agent systems where an agent needs to act with a specific user's authorization rather than with system-level access. Instead of the agent holding credentials that grant broad access, OBO sessions tie the agent's credentials to the identity and permission tier of the user who initiated the operation.

This matters in multi-tenant systems where agents serve multiple users and where the data a user can access varies by their role or clearance level.

---

## The problem OBO solves

Consider an agent that retrieves documents from a provider on behalf of users. If the agent holds a single system-level credential, it can retrieve documents from any user's account. The agent's authorization is entirely separate from the user's authorization. If the agent is compromised or makes an error, there is no connection between what the agent accessed and what the user actually has permission to see.

OBO sessions solve this by tying the agent's session to the originating user. The broker validates the user's identity token, extracts the user's identity and clearance level, and stamps those onto the session. The agent can then only access what that specific user is authorized to access, not the maximum of what the agent's own credentials allow.

---

## How to create an OBO session

Your backend validates the user's incoming identity token from your IdP (Okta, Microsoft Entra, or your own). It then calls the Nexus broker, passing the validated user context token:

```bash
curl -s -X POST http://localhost:8090/v1/obo-sessions \
  -H "Content-Type: application/json" \
  -H "X-Agent-ID: ops-agent" \
  -H "X-User-Context-Token: <validated-user-token>" \
  -d '{
    "provider": "internal-ops",
    "scopes": ["acme:documents:read"]
  }'
```

The broker validates the request and returns an OBO session:

```json
{
  "session_id": "ses_01HXYZ...",
  "access_token": "eyJ...",
  "expires_at": "2026-05-12T14:30:00Z",
  "acting_for": "user:alice@acme.com",
  "tenant_id": "tenant:acme-corp",
  "clearance_level": "L2"
}
```

The `acting_for`, `tenant_id`, and `clearance_level` fields describe the delegation chain. Your agent uses the `access_token` to make requests to the provider and includes the OBO context in any downstream logging or audit records.

---

## The clearance level is fixed at session creation

Once the broker issues an OBO session, the clearance level cannot be changed for that session. An agent that received an L2 delegation cannot request L3 resources, even if its own registered scopes include them. The broker enforces this at the session layer before issuing the token.

This is not a limitation. It is the point. An OBO session is a capability grant bounded by the user who authorized the operation. An agent that could escalate its own clearance within a user session would be able to access resources the user themselves cannot reach, which defeats the purpose of delegation.

---

## Session scope validation

The broker validates the requested scopes against two independent boundaries: the agent's registered scope list and the user's permission set derived from the context token. The session is granted only the intersection. If the agent requests `acme:documents:write` but the user only has read permission, the session is created with read scope only.

The broker rejects the request entirely if the agent requests scopes that appear in neither the agent's registered list nor the user's permission set.

---

## Audit logging

Every OBO session appears in the audit log with the full delegation chain: the originating user, the agent ID, the provider, the scopes granted, the clearance level, and the session timestamps. This gives your compliance team a complete, tamper-evident record of every user-initiated agent action without requiring you to instrument agents individually.

---

## Using OBO sessions from Go

```go
import oauthsdk "github.com/Prescott-Data/nexus-framework/nexus-sdk"

client := oauthsdk.New("https://nexus-gateway.example.com")

session, err := client.RequestOBOSession(ctx, oauthsdk.OBOSessionInput{
    AgentID:          "ops-agent",
    Provider:         "internal-ops",
    Scopes:           []string{"acme:documents:read"},
    UserContextToken: userToken,
})
if err != nil {
    return err
}

// session.AccessToken, session.ActingFor, session.ClearanceLevel
defer client.CloseOBOSession(ctx, session.SessionID)
```

Call `CloseOBOSession` when the operation is complete. The broker revokes the token server-side, which means it cannot be replayed if it is intercepted after the agent finishes its work.
