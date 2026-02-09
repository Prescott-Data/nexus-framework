-- Backfill metadata for known providers based on frontend requirements

-- Google
UPDATE provider_profiles 
SET api_base_url = 'https://www.googleapis.com', user_info_endpoint = '/oauth2/v3/userinfo' 
WHERE name = 'google';

-- Microsoft (Graph)
UPDATE provider_profiles 
SET api_base_url = 'https://graph.microsoft.com/v1.0', user_info_endpoint = '/me' 
WHERE name = 'microsoft-graph';

-- Microsoft (Generic/Legacy)
UPDATE provider_profiles 
SET api_base_url = 'https://graph.microsoft.com', user_info_endpoint = '/v1.0/me' 
WHERE name = 'microsoft';

-- GitHub
UPDATE provider_profiles 
SET api_base_url = 'https://api.github.com', user_info_endpoint = '/user' 
WHERE name = 'github';

-- GitLab
UPDATE provider_profiles 
SET api_base_url = 'https://gitlab.com/api/v4', user_info_endpoint = '/user' 
WHERE name = 'gitlab';

-- Slack
UPDATE provider_profiles 
SET api_base_url = 'https://slack.com/api', user_info_endpoint = '/users.identity' 
WHERE name = 'slack';

-- Discord
UPDATE provider_profiles 
SET api_base_url = 'https://discord.com/api', user_info_endpoint = '/users/@me' 
WHERE name = 'discord';

-- Dropbox
UPDATE provider_profiles 
SET api_base_url = 'https://api.dropboxapi.com/2', user_info_endpoint = '/users/get_current_account' 
WHERE name = 'dropbox';

-- Box
UPDATE provider_profiles 
SET api_base_url = 'https://api.box.com/2.0', user_info_endpoint = '/users/me' 
WHERE name = 'box';

-- Asana
UPDATE provider_profiles 
SET api_base_url = 'https://app.asana.com/api/1.0', user_info_endpoint = '/users/me' 
WHERE name = 'asana';

-- ClickUp
UPDATE provider_profiles 
SET api_base_url = 'https://api.clickup.com/api/v2', user_info_endpoint = '/user' 
WHERE name = 'clickup';

-- Airtable
UPDATE provider_profiles 
SET api_base_url = 'https://api.airtable.com/v0', user_info_endpoint = '/meta/whoami', auth_header = 'client_secret_basic' 
WHERE name = 'airtable';

-- Monday
UPDATE provider_profiles 
SET api_base_url = 'https://api.monday.com/v2', user_info_endpoint = '/me' 
WHERE name = 'monday';

-- Notion
UPDATE provider_profiles 
SET api_base_url = 'https://api.notion.com/v1', user_info_endpoint = '/users/me' 
WHERE name = 'notion';

-- Linear
UPDATE provider_profiles 
SET api_base_url = 'https://api.linear.app', user_info_endpoint = '/me' 
WHERE name = 'linear';

-- Pipedrive
UPDATE provider_profiles 
SET api_base_url = 'https://api.pipedrive.com/v1', user_info_endpoint = '/users/me' 
WHERE name = 'pipedrive';

-- HubSpot
UPDATE provider_profiles 
SET api_base_url = 'https://api.hubapi.com', user_info_endpoint = '/oauth/v1/access-tokens/info' 
WHERE name = 'hubspot';

-- Trello
UPDATE provider_profiles 
SET api_base_url = 'https://api.trello.com/1', user_info_endpoint = '/members/me' 
WHERE name = 'trello';

-- Zoho
UPDATE provider_profiles 
SET api_base_url = 'https://www.zohoapis.com', user_info_endpoint = '/crm/v3/users' 
WHERE name = 'zoho';

-- Xero
UPDATE provider_profiles 
SET api_base_url = 'https://api.xero.com/api.xro/2.0', user_info_endpoint = '/Users' 
WHERE name = 'xero';

-- QuickBooks
UPDATE provider_profiles 
SET api_base_url = 'https://quickbooks.api.intuit.com/v3/company', user_info_endpoint = '/{companyId}/user' 
WHERE name = 'quickbooks';

-- FreshBooks
UPDATE provider_profiles 
SET api_base_url = 'https://api.freshbooks.com', user_info_endpoint = '/auth/api/v1/users/me' 
WHERE name = 'freshbooks';

-- Okta
UPDATE provider_profiles 
SET api_base_url = 'https://okta.com', user_info_endpoint = '/api/v1/users/me' 
WHERE name = 'okta';

-- Webex
UPDATE provider_profiles 
SET api_base_url = 'https://webexapis.com/v1', user_info_endpoint = '/people/me' 
WHERE name = 'webex';

-- LinkedIn
UPDATE provider_profiles 
SET api_base_url = 'https://api.linkedin.com/v2', user_info_endpoint = '/me' 
WHERE name = 'linkedin';

-- Twitter
UPDATE provider_profiles 
SET api_base_url = 'https://api.twitter.com/2', user_info_endpoint = '/users/me' 
WHERE name = 'twitter';

-- Reddit
UPDATE provider_profiles 
SET api_base_url = 'https://oauth.reddit.com', user_info_endpoint = '/api/v1/me' 
WHERE name = 'reddit';

-- Shopify
UPDATE provider_profiles 
SET api_base_url = 'https://{shop}.myshopify.com/admin/api/2023-04', user_info_endpoint = '/shop.json' 
WHERE name = 'shopify';

-- YouTube
UPDATE provider_profiles 
SET api_base_url = 'https://www.googleapis.com/youtube/v3', user_info_endpoint = '/channels?mine=true&part=snippet' 
WHERE name = 'youtube';
