import React from 'react'
import ReactDOM from 'react-dom/client'
import App from './App.tsx'
import './index.css'

declare global {
  interface Window {
    NONCE_FROM_SPASERVE: string;
  }
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <App cspNonce={window.NONCE_FROM_SPASERVE} />
  </React.StrictMode>,
)
