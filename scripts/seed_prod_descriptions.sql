-- Production provider descriptions seed — covers all 154 providers
-- Named providers get accurate descriptions; any others get a sensible
-- auto-generated fallback from their provider name.
-- Run with: psql $DATABASE_URL -f seed_prod_descriptions.sql

UPDATE provider_profiles
SET description = CASE name

  -- ── API Key providers ──────────────────────────────────────────────────────
  WHEN 'prembly'                        THEN 'Connect Prembly to run KYC checks, document verification, and identity validation workflows for compliance-driven onboarding.'
  WHEN 'sourcegraph'                    THEN 'Connect Sourcegraph to search and navigate large codebases, automate code intelligence workflows, and surface insights across repositories.'
  WHEN 'teamcity'                       THEN 'Connect TeamCity to trigger CI/CD pipelines, monitor build statuses, and automate deployment workflows from your enterprise build server.'
  WHEN 'telegram'                       THEN 'Connect Telegram to send automated messages, manage bots, and deliver real-time notifications directly to users or groups.'
  WHEN 'corecommerce'                   THEN 'Connect CoreCommerce to sync product catalogs, automate order processing, and manage your online store data across systems.'
  WHEN 'travis-ci'                      THEN 'Connect Travis CI to trigger builds, monitor test pipelines, and automate deployment workflows tied to your GitHub repositories.'
  WHEN 'twilio'                         THEN 'Connect Twilio to automate SMS alerts, voice calls, and programmable messaging across customer-facing communication workflows.'
  WHEN 'close'                          THEN 'Connect Close CRM to automate lead follow-ups, sync sales activity, and trigger outreach workflows from pipeline events.'
  WHEN 'what3words'                     THEN 'Connect What3words to resolve precise 3-word addresses into GPS coordinates for logistics, delivery, and location-based automation workflows.'
  WHEN 'agilecrm'                       THEN 'Connect Agile CRM to automate contact management, sync deal pipelines, and trigger marketing and sales workflows.'
  WHEN 'africas-talking'                THEN 'Connect Africa''s Talking to send SMS, USSD, and voice messages across African telecom networks for customer engagement workflows.'
  WHEN 'height'                         THEN 'Connect Height to manage tasks, automate sprint workflows, and sync project updates across your engineering team.'
  WHEN 'amplitude'                      THEN 'Connect Amplitude to pull product analytics, track user behaviour events, and trigger data-driven automation from usage patterns.'
  WHEN 'opencart'                       THEN 'Connect OpenCart to sync product inventory, automate order fulfilment, and manage your e-commerce store data in real time.'
  WHEN 'plausible'                      THEN 'Connect Plausible to pull privacy-friendly web analytics and trigger workflows based on traffic spikes or goal completions.'
  WHEN 'coda'                           THEN 'Connect Coda to read and write docs, automate table updates, and sync structured data from your Coda workspaces.'
  WHEN 'proofhub'                       THEN 'Connect ProofHub to manage projects, assign tasks, and automate team collaboration workflows across your organisation.'
  WHEN 'heap'                           THEN 'Connect Heap to extract user behaviour analytics, track event data, and trigger workflows based on product usage signals.'
  WHEN 'nutshell'                       THEN 'Connect Nutshell CRM to automate lead management, sync contacts, and trigger sales pipeline workflows from deal updates.'
  WHEN 'drift'                          THEN 'Connect Drift to automate conversational marketing workflows, sync lead data from chats, and trigger CRM updates from sales conversations.'
  WHEN 'hubspot-api'                    THEN 'Connect HubSpot to automate CRM updates, sync marketing contacts, and trigger workflows from deal stage changes and form submissions.'
  WHEN 'mattermost'                     THEN 'Connect Mattermost to post automated messages, manage channels, and trigger team notifications from enterprise workflow events.'
  WHEN 'mixpanel'                       THEN 'Connect Mixpanel to extract event analytics, track conversion funnels, and trigger automated workflows from user behaviour data.'
  WHEN 'salesflare'                     THEN 'Connect Salesflare to automate B2B CRM updates, sync contact timelines, and trigger follow-up workflows from sales signals.'
  WHEN 'segment'                        THEN 'Connect Segment to route customer event data across analytics and marketing tools, and trigger workflows from real-time user actions.'
  WHEN 'targetprocess'                  THEN 'Connect Targetprocess to manage agile projects, sync team capacity, and automate portfolio-level planning workflows.'
  WHEN 'amazon-s3'                      THEN 'Connect Amazon S3 to automate file uploads, manage bucket contents, and trigger data processing workflows from storage events.'
  WHEN 'freshchat'                      THEN 'Connect Freshchat to automate customer messaging workflows, sync conversation data, and trigger support escalations from chat events.'
  WHEN 'fullstory'                      THEN 'Connect FullStory to access session replay data, track user friction points, and trigger workflows from digital experience signals.'
  WHEN 'gorgias'                        THEN 'Connect Gorgias to automate e-commerce customer support workflows, sync ticket data, and trigger responses from order and refund events.'
  WHEN 'matomo'                         THEN 'Connect Matomo to pull privacy-respecting web analytics, track campaign performance, and trigger workflows from visitor behaviour.'
  WHEN 'olark'                          THEN 'Connect Olark to sync live chat transcripts, automate lead capture from conversations, and trigger CRM updates from chat events.'
  WHEN 'pivotal-tracker'                THEN 'Connect Pivotal Tracker to manage agile stories, automate sprint workflows, and sync velocity data across your development team.'
  WHEN 'tawkto'                         THEN 'Connect Tawk.to to sync live chat conversations, automate visitor engagement workflows, and capture leads from support interactions.'
  WHEN 'klaviyo'                        THEN 'Connect Klaviyo to automate e-commerce email and SMS campaigns, sync customer segments, and trigger flows from purchase events.'
  WHEN 'perforce'                       THEN 'Connect Perforce to manage version-controlled assets, automate branching workflows, and sync large-file repositories across engineering teams.'
  WHEN 'trello'                         THEN 'Connect Trello to automate card creation, sync board updates, and trigger project management workflows from task state changes.'
  WHEN 'activecampaign'                 THEN 'Connect ActiveCampaign to automate email marketing sequences, sync CRM contact data, and trigger workflows from subscriber behaviour.'
  WHEN 'clay'                           THEN 'Connect Clay to enrich lead data, automate prospecting workflows, and build personalised outreach sequences from your contact lists.'
  WHEN 'paystack'                       THEN 'Connect Paystack to automate payment collection, sync transaction data, and trigger fulfilment workflows from successful charges.'
  WHEN 'slack-bot'                      THEN 'Connect Slack Bot to post automated notifications, respond to commands, and trigger team workflows directly from your Slack workspace.'
  WHEN 'workfront'                      THEN 'Connect Workfront to manage enterprise projects, automate resource allocation, and sync work management data across teams.'
  WHEN 'sendgrid'                       THEN 'Connect SendGrid to automate transactional emails, manage contact lists, and trigger notification workflows from application events.'
  WHEN 'phabricator'                    THEN 'Connect Phabricator to manage code reviews, sync task queues, and automate engineering collaboration workflows.'
  WHEN 'shortcut'                       THEN 'Connect Shortcut to manage stories and epics, automate sprint planning, and sync engineering progress across your product team.'
  WHEN 'streak'                         THEN 'Connect Streak to automate Gmail-based CRM workflows, sync pipeline stages, and trigger follow-ups from email activity.'
  WHEN 'airtable-api'                   THEN 'Connect Airtable to read and write base records, automate table-driven workflows, and sync structured data across your operations.'
  WHEN 'svn'                            THEN 'Connect Subversion (SVN) to manage code repositories, automate commit-triggered workflows, and sync version-controlled assets.'
  WHEN 'woocommerce'                    THEN 'Connect WooCommerce to sync orders, automate fulfilment workflows, and manage your WordPress store data in real time.'

  -- ── Basic Auth providers ───────────────────────────────────────────────────
  WHEN 'openmrs'                        THEN 'Connect OpenMRS to access patient records, automate clinical data workflows, and sync healthcare information across medical systems.'
  WHEN 'backblaze-b2'                   THEN 'Connect Backblaze B2 to automate cloud storage uploads, manage object buckets, and trigger data archival workflows.'

  -- ── OAuth2 providers ───────────────────────────────────────────────────────
  WHEN 'azure-blob'                     THEN 'Connect Azure Blob Storage to automate file management, sync large datasets, and trigger cloud processing workflows from storage events.'
  WHEN 'gitlab'                         THEN 'Connect GitLab to automate CI/CD pipelines, sync repository events, and trigger workflows from merge requests and issue updates.'
  WHEN 'podio'                          THEN 'Connect Podio to manage custom apps, automate workflow items, and sync collaborative workspace data across your organisation.'
  WHEN 'shift4shop'                     THEN 'Connect Shift4Shop to sync e-commerce orders, manage product catalogues, and automate customer and inventory workflows.'
  WHEN 'shopify'                        THEN 'Connect Shopify to automate order fulfilment, sync product and customer data, and trigger e-commerce workflows from store events.'
  WHEN 'wix-stores'                     THEN 'Connect Wix Stores to sync product listings, automate order management workflows, and integrate your Wix storefront with backend systems.'
  WHEN 'snapchat-ads'                   THEN 'Connect Snapchat Ads to automate ad campaign management, sync performance metrics, and trigger optimisation workflows from ad events.'
  WHEN 'tiktok'                         THEN 'Connect TikTok to automate content publishing, sync video analytics, and trigger creator and marketing workflows from platform events.'
  WHEN 'google-maps'                    THEN 'Connect Google Maps to access geolocation data, automate address validation, and enrich workflows with mapping and distance intelligence.'
  WHEN 'jira'                           THEN 'Connect Jira to automate issue tracking, sync sprint progress, and trigger development workflows from ticket state changes.'
  WHEN 'notion'                         THEN 'Connect Notion to read and write pages and databases, automate documentation workflows, and sync team knowledge across your workspace.'
  WHEN 'okta'                           THEN 'Connect Okta to automate identity management, sync user provisioning, and trigger access control workflows from authentication events.'
  WHEN 'reddit'                         THEN 'Connect Reddit to automate post submissions, monitor community threads, and trigger workflows from subreddit activity and engagement signals.'
  WHEN 'zoho-books'                     THEN 'Connect Zoho Books to automate invoicing, sync financial records, and trigger accounting workflows from transaction and payment events.'
  WHEN 'capsulecrm'                     THEN 'Connect Capsule CRM to automate contact and opportunity management, sync sales data, and trigger follow-up workflows from pipeline updates.'
  WHEN 'dynamics365'                    THEN 'Connect Dynamics 365 to automate CRM and ERP workflows, sync customer records, and trigger business process automation across Microsoft cloud.'
  WHEN 'intercom'                       THEN 'Connect Intercom to automate customer messaging, sync contact and conversation data, and trigger support workflows from user lifecycle events.'
  WHEN 'linear'                         THEN 'Connect Linear to automate issue tracking, sync engineering cycles, and trigger workflows from project and milestone updates.'
  WHEN 'quickbooks'                     THEN 'Connect QuickBooks to automate bookkeeping workflows, sync invoices and expenses, and trigger financial reporting from accounting events.'
  WHEN 'sage-pastel'                    THEN 'Connect Sage Pastel to automate accounting workflows, sync financial data, and manage business transactions across your Sage environment.'
  WHEN 'freshbooks'                     THEN 'Connect FreshBooks to automate invoicing, sync expense data, and trigger accounting workflows from project billing and payment events.'
  WHEN 'etsy'                           THEN 'Connect Etsy to automate listing management, sync order data, and trigger fulfilment workflows from your Etsy seller account.'
  WHEN 'google-drive'                   THEN 'Connect Google Drive to automate file management, sync document workflows, and trigger processing pipelines from Drive folder events.'
  WHEN 'pagerduty'                      THEN 'Connect PagerDuty to automate incident management, sync on-call schedules, and trigger escalation workflows from alert and service events.'
  WHEN 'zoho-people'                    THEN 'Connect Zoho People to automate HR workflows, sync employee records, and trigger leave and attendance management processes.'
  WHEN 'bamboo'                         THEN 'Connect Bamboo HR to automate employee onboarding, sync HR data, and trigger people operations workflows from workforce events.'
  WHEN 'reddit-ads'                     THEN 'Connect Reddit Ads to automate ad campaign management, sync performance data, and trigger optimisation workflows from advertising events.'
  WHEN 'slack'                          THEN 'Connect Slack to automate team notifications, manage channel messages, and trigger collaborative workflows from workspace events.'
  WHEN 'zoho-desk'                      THEN 'Connect Zoho Desk to automate customer support ticketing, sync agent activity, and trigger escalation workflows from ticket status changes.'
  WHEN 'circleci'                       THEN 'Connect CircleCI to automate CI/CD pipelines, monitor build and test workflows, and trigger deployments from code commit events.'
  WHEN 'discord'                        THEN 'Connect Discord to automate server notifications, manage community channels, and trigger bot workflows from guild and message events.'
  WHEN 'monday'                         THEN 'Connect Monday.com to automate project boards, sync item updates, and trigger team workflows from column and status changes.'
  WHEN '3dcart'                         THEN 'Connect 3dcart to sync e-commerce product and order data, automate store operations, and trigger fulfilment workflows from purchase events.'
  WHEN 'assembla'                       THEN 'Connect Assembla to manage code repositories and tickets, automate team workflows, and sync project activity across engineering pipelines.'
  WHEN 'linkedin-ads'                   THEN 'Connect LinkedIn Ads to automate B2B ad campaigns, sync performance metrics, and trigger lead generation workflows from ad engagement.'
  WHEN 'snapchat'                       THEN 'Connect Snapchat to sync user profile data, automate creator workflows, and trigger marketing automations from platform interactions.'
  WHEN 'square'                         THEN 'Connect Square to automate payment processing, sync point-of-sale data, and trigger fulfilment and inventory workflows from transactions.'
  WHEN 'zoho'                           THEN 'Connect Zoho CRM to automate lead and deal management, sync contact records, and trigger sales workflows from CRM module events.'
  WHEN 'google-analytics'               THEN 'Connect Google Analytics to pull traffic and conversion data, monitor campaign performance, and trigger workflows from goal and event completions.'
  WHEN 'keap'                           THEN 'Connect Keap to automate CRM and email marketing workflows, sync contact data, and trigger follow-up sequences from sales events.'
  WHEN 'egnyte'                         THEN 'Connect Egnyte to automate enterprise file management, sync content workflows, and trigger governance and collaboration processes from storage events.'
  WHEN 'jumia-seller-center'            THEN 'Connect Jumia Seller Center to sync product listings, automate order management, and integrate your storefront with backend fulfilment systems.'
  WHEN 'sage-business-cloud-accounting' THEN 'Connect Sage Business Cloud Accounting to automate bookkeeping, sync invoices and payments, and trigger financial workflows from accounting events.'
  WHEN 'webex'                          THEN 'Connect Webex to automate meeting scheduling, send team messages, and trigger collaboration workflows from Webex space and room events.'
  WHEN 'youtube'                        THEN 'Connect YouTube to automate video publishing, sync channel analytics, and trigger content workflows from upload and engagement events.'
  WHEN 'wave-accounting'                THEN 'Connect Wave Accounting to automate invoicing and expense tracking, sync financial records, and trigger accounting workflows for small business operations.'
  WHEN 'todoist'                        THEN 'Connect Todoist to automate task creation, sync project data, and trigger productivity workflows from task completion and due date events.'
  WHEN 'twitter'                        THEN 'Connect Twitter (X) to automate tweet publishing, monitor mentions, and trigger social media workflows from engagement and timeline events.'
  WHEN 'atlassian'                      THEN 'Connect Atlassian to automate workflows across Jira, Confluence, and Bitbucket, syncing project and documentation data across your development lifecycle.'
  WHEN 'gcs'                            THEN 'Connect Google Cloud Storage to automate object management, trigger data processing pipelines from bucket events, and sync files across cloud workflows.'
  WHEN 'linkedin'                       THEN 'Connect LinkedIn to sync professional profile data, automate social outreach workflows, and trigger lead generation from network activity.'
  WHEN 'safaricom-mpesa'                THEN 'Connect Safaricom M-Pesa to automate mobile money payments, sync transaction records, and trigger fulfilment workflows from payment confirmations.'

  -- ── Fallback for any provider not explicitly listed above ─────────────────
  -- Generates a sensible description from the provider name so all 154 rows
  -- are covered even if their names were hidden by unstable pagination.
  ELSE CONCAT(
    'Connect ',
    INITCAP(REPLACE(REPLACE(name, '-', ' '), '_', ' ')),
    ' to automate enterprise workflows, sync data across systems, and drive smarter operations with real-time insights.'
  )

END
WHERE deleted_at IS NULL
  AND (description IS NULL OR description = '');
