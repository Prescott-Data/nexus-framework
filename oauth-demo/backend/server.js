const express = require('express');
const cors = require('cors');
const morgan = require('morgan');
const axios = require('axios');

const app = express();
const port = 3001;

// --- In-memory store for the demo ---
// In a real application, you would use a database.
const connectionStore = {
  connectionId: null,
};
// -----------------------------------------

const GATEWAY_URL = 'https://dromos-oauth-gateway.bravesea-3f5f7e75.eastus.azurecontainerapps.io';
// The URL of this backend server, which will be used for the OAuth callback.
// Make sure this is reachable by the user's browser. For local dev, 
// this might require a tool like ngrok if the broker can't reach localhost.
const BACKEND_URL = 'http://localhost:3001';


app.use(cors());
app.use(morgan('dev'));
app.use(express.json());

/**
 * STEP 1: Request a connection from the Dromos Gateway.
 * The frontend calls this endpoint to start the OAuth flow.
 */
app.post('/api/request-connection', async (req, res) => {
  console.log('Received request to /api/request-connection');
  try {
    const response = await axios.post(`${GATEWAY_URL}/v1/request-connection`, {
      user_id: 'demo-user-123', // A static user ID for this demo
      provider_name: 'Google',
      scopes: ['openid', 'email', 'profile'],
      return_url: `${BACKEND_URL}/callback`, // The crucial redirect back to our server
    });

    console.log('Successfully got authUrl from gateway:', response.data.authUrl);
    res.json({ authUrl: response.data.authUrl });
  } catch (error) {
    console.error('Error requesting connection from gateway:', error.response ? error.response.data : error.message);
    res.status(500).json({ message: 'Failed to request connection from gateway.' });
  }
});

/**
 * STEP 2: Handle the callback from the Dromos Broker.
 * The user is redirected here by the broker after successful consent.
 */
app.get('/callback', (req, res) => {
  const { connection_id, status } = req.query;
  console.log(`Received callback with status: ${status} and connection_id: ${connection_id}`);

  if (status === 'success' && connection_id) {
    // Store the connection_id.
    connectionStore.connectionId = connection_id;
    // Redirect the user's browser back to the frontend app.
    // We'll pass the connection_id back so the frontend knows it was successful.
    res.redirect(`http://localhost:5173/?connection_id=${connection_id}`);
  } else {
    // Handle failure case
    res.redirect('http://localhost:5173/?error=authentication-failed');
  }
});

/**
 * STEP 3: Fetch the token from the Dromos Gateway.
 * The frontend calls this after it has a connection_id.
 */
app.get('/api/get-token', async (req, res) => {
  const { connectionId } = connectionStore;
  console.log(`Received request to /api/get-token for connectionId: ${connectionId}`);

  if (!connectionId) {
    return res.status(400).json({ message: 'No active connection found.' });
  }

  try {
    const response = await axios.get(`${GATEWAY_URL}/v1/token/${connectionId}`);
    console.log('Successfully got token from gateway.');
    res.json(response.data);
  } catch (error) {
    console.error('Error fetching token from gateway:', error.response ? error.response.data : error.message);
    res.status(500).json({ message: 'Failed to fetch token from gateway.' });
  }
});

/**
 * STEP 4: Use the token to get user info from the provider (Google).
 * This demonstrates that the access token is valid.
 */
app.get('/api/get-user-info', async (req, res) => {
    const { connectionId } = connectionStore;
    console.log(`Received request to /api/get-user-info for connectionId: ${connectionId}`);

    if (!connectionId) {
        return res.status(400).json({ message: 'No active connection found.' });
    }

    try {
        // 1. Get the token from the gateway first.
        const tokenResponse = await axios.get(`${GATEWAY_URL}/v1/token/${connectionId}`);
        const accessToken = tokenResponse.data.access_token;

        if (!accessToken) {
            return res.status(400).json({ message: 'Access token not found in gateway response.' });
        }

        // 2. Use the token to call the Google userinfo endpoint.
        const userInfoResponse = await axios.get('https://www.googleapis.com/oauth2/v3/userinfo', {
            headers: {
                'Authorization': `Bearer ${accessToken}`,
            },
        });
        
        console.log('Successfully got user info from Google.');
        res.json(userInfoResponse.data);

    } catch (error) {
        console.error('Error getting user info:', error.response ? error.response.data : error.message);
        res.status(500).json({ message: 'Failed to get user info.' });
    }
});


app.listen(port, () => {
  console.log(`Backend server listening at http://localhost:${port}`);
});
