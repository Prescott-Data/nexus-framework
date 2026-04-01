#!/bin/bash

GATEWAY_URL="https://nexus-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io"
TEST_USER="terminal-qa-$(date +%s)"
RETURN_URL="https://httpbin.org/get"

echo "------------------------------------------------------------"
echo "  Nexus Provider Test Link Generator"
echo "------------------------------------------------------------"
echo "Generating links for all OAuth2 providers..."
echo ""

# 1. Fetch provider names
PROVIDERS=$(curl -s "$GATEWAY_URL/v1/providers" | jq -r '.oauth2 | keys[]')

for name in $PROVIDERS; do
    if [ -z "$name" ]; then continue; fi
    
    echo -n "[...] $name: "
    
    # 2. Get registered scopes
    SCOPES=$(curl -s "$GATEWAY_URL/v1/providers" | jq -r ".oauth2[\"$name\"] .scopes | join(" ")")
    
    # Fallback to openid if no scopes
    if [ -z "$SCOPES" ]; then
        SCOPES="openid"
    fi
    
    # 3. Request Connection
    RESPONSE=$(curl -s -X POST "$GATEWAY_URL/v1/request-connection" \
      -H "Content-Type: application/json" \
      -d "{
        \"user_id\": \"$TEST_USER\",
        \"provider_name\": \"$name\",
        \"scopes\": [\"$(echo $SCOPES | sed 's/ /","/g')\"],
        \"return_url\": \"$RETURN_URL\"
      }")

    AUTH_URL=$(echo "$RESPONSE" | jq -r '.authUrl // empty')

    if [ ! -z "$AUTH_URL" ]; then
        echo -e "\e[32mSUCCESS\e[0m"
        echo "      Link: $AUTH_URL"
        echo ""
    else
        ERROR=$(echo "$RESPONSE" | jq -r '.message // "Unknown error"')
        echo -e "\e[31mFAILED\e[0m ($ERROR)"
    fi
done

echo "------------------------------------------------------------"
echo "Done. Click any link above to test in your browser."
