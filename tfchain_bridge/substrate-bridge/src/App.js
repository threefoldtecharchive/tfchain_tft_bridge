import logo from './logo.svg'
import './App.css'

import { useEffect, useState } from 'react'
import {
  web3Accounts,
  web3Enable,
  web3FromAddress,
} from '@polkadot/extension-dapp';
import { connect } from './connect'
import { Withdraw } from './components/withdraw'
import { Button } from '@material-ui/core'

function App() {
  const [api, setApi] = useState()
  const [balance, setBalance] = useState(0)
  const [account, setAccount] = useState()

  const [openWithdrawDialog, setOpenWithdrawDialog] = useState(false)
  const [loadingWithdrawal, setLoadingWithdrawal] = useState(false)
  const handleCloseWithdrawDialog = () => setOpenWithdrawDialog(false)

  useEffect(() => {
    connect()
      .then(api => {
        setApi(api)
        web3Enable('TF Chain Bridge UI')
        web3Accounts().then(accounts => {
          console.log(accounts)
          setAccount(accounts[0])
          api.query.system.account(accounts[0].address)
            .then(balance => {
              console.log(balance.data.free.toJSON())
              setBalance(balance.data.free.toJSON())
            })
        })
      })
  }, [])

  const transfer = (stellarAddress, amount) => {
    setLoadingWithdrawal(true)
    handleCloseWithdrawDialog()

    web3FromAddress(account.address)
      .then(injector => {
        api.tx.tftBridgeModule
          .swapToStellar('5HGjWAeFDfFCWPsjFQdVV2Msvz2XtMktvgocEZcCj68kUMaw', amount*1e6)
          .signAndSend(account.address, { signer: injector.signer }, (status) => {
            console.log(status)
            setLoadingWithdrawal(false)
          })
      })
  }

  return (
    <div className="App">
      <header className="App-header">
        <img src={logo} className="App-logo" alt="logo" />
        <Button style={{ width: '50%', marginTop: 20, alignSelf: 'center' }} color='default' variant='outlined' onClick={() => setOpenWithdrawDialog(true)}>
          Withdraw to Stellar
        </Button>
        <Withdraw
          open={openWithdrawDialog}
          handleClose={handleCloseWithdrawDialog}
          balance={balance}
          submitWithdraw={transfer}
        />
      </header>
    </div>
  )
}

export default App
