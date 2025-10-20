import { useState, useEffect } from 'react';
import axios from 'axios';
import './App.css';

// The base URL of our backend server
const API_URL = 'http://localhost:3001';

function App() {
  const [connectionId, setConnectionId] = useState(null);
  const [tokenData, setTokenData] = useState(null);
  const [userInfo, setUserInfo] = useState(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  // On component mount, check if we've been redirected back from the OAuth flow
  useEffect(() => {
    const urlParams = new URLSearchParams(window.location.search);
    const connectionIdFromUrl = urlParams.get('connection_id');
    const errorFromUrl = urlParams.get('error');

    if (connectionIdFromUrl) {
      setConnectionId(connectionIdFromUrl);
      // Clean the URL
      window.history.replaceState({}, document.title, "/");
    } else if (errorFromUrl) {
      setError('Authentication failed. Please try again.');
      // Clean the URL
      window.history.replaceState({}, document.title, "/");
    }
  }, []);

  const handleConnect = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await axios.post(`${API_URL}/api/request-connection`);
      // Redirect the user to the provider's consent screen
      window.location.href = response.data.authUrl;
    } catch (err) {
      setError('Failed to start the connection process.');
      console.error(err);
    }
    setLoading(false);
  };

  const handleGetToken = async () => {
    setLoading(true);
    setError(null);
    try {
      const response = await axios.get(`${API_URL}/api/get-token`);
      setTokenData(response.data);
    } catch (err) {
      setError('Failed to fetch the token.');
      console.error(err);
    }
    setLoading(false);
  };

  const handleGetUserInfo = async () => {
    setLoading(true);
    setError(null);
    try {
        const response = await axios.get(`${API_URL}/api/get-user-info`);
        setUserInfo(response.data);
    } catch (err) {
        setError('Failed to fetch user info.');
        console.error(err);
    }
    setLoading(false);
  };

  return (
    <div className="container mt-5">
      <header className="text-center mb-4">
        <h1>Nexus Protocol Demo</h1>
        <p className="lead">A visual demonstration of the end-to-end OAuth flow.</p>
      </header>

      {error && <div className="alert alert-danger">{error}</div>}

      <div className="card shadow-sm">
        <div className="card-body">
          {/* STEP 1: INITIATE CONNECTION */}
          <div className="step">
            <h5 className="card-title">Step 1: Initiate Connection</h5>
            <p className="card-text">Click the button to start the OAuth 2.0 flow. This will call the backend, which in turn calls the Dromos Gateway to get a unique authorization URL.</p>
            <button className="btn btn-primary" onClick={handleConnect} disabled={loading || connectionId}>
              {loading ? 'Loading...' : 'Connect with Google'}
            </button>
          </div>

          <hr />

          {/* STEP 2: DISPLAY CONNECTION ID */}
          <div className={`step ${!connectionId ? 'disabled' : ''}`}>
            <h5 className="card-title">Step 2: Connection Established</h5>
            <p className="card-text">Success! The OAuth flow is complete. The backend has received the callback and stored the unique `connection_id` for this session.</p>
            {connectionId ? (
              <div className="alert alert-success">
                <strong>Connection ID:</strong> {connectionId}
              </div>
            ) : (
              <p className="text-muted"><i>Waiting for connection...</i></p>
            )}
          </div>

          <hr />

          {/* STEP 3: FETCH TOKEN */}
          <div className={`step ${!connectionId ? 'disabled' : ''}`}>
            <h5 className="card-title">Step 3: Fetch Access Token</h5>
            <p className="card-text">Now, use the `connection_id` to securely fetch the access token from the Dromos Gateway via our backend.</p>
            <button className="btn btn-secondary" onClick={handleGetToken} disabled={loading || !connectionId}>
              Fetch Token
            </button>
            {tokenData && (
              <div className="mt-3">
                <h6>Token Data:</h6>
                <pre className="bg-light p-3 rounded"><code>{JSON.stringify(tokenData, null, 2)}</code></pre>
              </div>
            )}
          </div>

          <hr />

          {/* STEP 4: USE THE TOKEN */}
          <div className={`step ${!tokenData ? 'disabled' : ''}`}>
            <h5 className="card-title">Step 4: Use the Token</h5>
            <p className="card-text">Finally, use the access token to make an authenticated API call to a Google endpoint to prove it's valid.</p>
            <button className="btn btn-success" onClick={handleGetUserInfo} disabled={loading || !tokenData}>
              Get User Info from Google
            </button>
            {userInfo && (
                <div className="mt-3">
                    <h6>Google User Info:</h6>
                    <pre className="bg-light p-3 rounded"><code>{JSON.stringify(userInfo, null, 2)}</code></pre>
                </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
}

export default App;