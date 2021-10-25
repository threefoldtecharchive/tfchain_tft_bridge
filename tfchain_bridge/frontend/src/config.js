const config = {
  REACT_APP_API_URL: window.REACT_APP_API_URL || process.env.REACT_APP_API_URL,
  REACT_APP_BRIDGE_TFT_ADDRESS: window.REACT_APP_BRIDGE_TFT_ADDRESS || process.env.REACT_APP_BRIDGE_TFT_ADDRESS,
  REACT_APP_STELLAR_HORIZON_URL: window.REACT_APP_STELLAR_HORIZON_URL || process.env.REACT_APP_STELLAR_HORIZON_URL,
  REACT_APP_TFT_ASSET_ISSUER: window.REACT_APP_TFT_ASSET_ISSUER || process.REACT_APP_TFT_ASSET_ISSUER
}

export default config