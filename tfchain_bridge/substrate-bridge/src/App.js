import logo from './3fold_logo.png'
import './App.css'

import { useEffect, useState } from 'react'
import {
  web3Accounts,
  web3Enable,
  web3FromAddress,
} from '@polkadot/extension-dapp';
import { connect } from './connect'
import { Button } from '@material-ui/core'
import { Withdraw } from './components/withdraw'
import { Balance } from './components/balance'
import { Spinner } from './components/spinner'
import { useSnackbar } from 'notistack'

function App() {
  const [api, setApi] = useState()
  const [balance, setBalance] = useState(0)
  const [account, setAccount] = useState()

  const [openWithdrawDialog, setOpenWithdrawDialog] = useState(false)
  const [loadingWithdrawal, setLoadingWithdrawal] = useState(false)
  const handleCloseWithdrawDialog = () => setOpenWithdrawDialog(false)

  const { enqueueSnackbar } = useSnackbar()

  useEffect(() => {
    console.log('effect triggered')
    connect()
      .then(api => {
        setApi(api)
        web3Enable('TF Chain Bridge UI').then(() => {
          web3Accounts().then(accounts => {
            console.log(accounts)
            enqueueSnackbar('Connection successfull')
            setAccount(accounts[0])
          })
        })
      })
  }, [enqueueSnackbar])

  const getBalance = () => {
    console.log('get balance triggered')
    api.query.system.account(account.address)
      .then(balance => {
        console.log(balance.data.free.toJSON())
        setBalance(balance.data.free.toJSON())
      })
  }

  useEffect(() => {
    if (api && account) {
      getBalance()
    }
  }, [api, account])

  const transfer = (stellarAddress, amount) => {
    setLoadingWithdrawal(true)
    handleCloseWithdrawDialog()

    web3FromAddress(account.address)
      .then(injector => {
        api.tx.tftBridgeModule
          .swapToStellar(stellarAddress, amount*1e7)
          .signAndSend(account.address, { signer: injector.signer }, (res) => {
            console.log(res)
            if (res instanceof Error) {
              console.log(res)
              setLoadingWithdrawal(false)
              return
            }
            const { events = [], status } = res
            console.log(`Current status is ${status.type}`)
          
            if (status.isFinalized) {
              console.log(`Transaction included at blockHash ${status.asFinalized}`)
          
              // Loop through Vec<EventRecord> to display all events
              events.forEach(({ phase, event: { data, method, section } }) => {
                console.log(`\t' ${phase}: ${section}.${method}:: ${data}`)
                if (section === 'tftBridgeModule' && method === 'BurnTransactionCreated') {
                  setLoadingWithdrawal(false)
                  getBalance(account)
                  enqueueSnackbar('Withdraw successfull')
                } else if (section === 'system' && method === 'ExtrinsicFailed') {
                  setLoadingWithdrawal(false)
                  getBalance(account)
                  enqueueSnackbar('Withdraw failed!')
                }
              })
            }
          })
      })
  }

  return (
    <div className="App">
      <header className="App-header">
        <img src={logo} className="App-logo" alt="logo" />
        <Balance balance={balance} />
        {loadingWithdrawal ? (
          <Spinner color={'black'} style={{ height: '25%', marginLeft: '-1rem' }} />
        ): (
          <Button style={{ width: '50%', marginTop: 20, alignSelf: 'center', backgroundColor: 'white' }} color='default' variant='outlined' onClick={() => setOpenWithdrawDialog(true)}>
            Withdraw to Stellar
          </Button>
        )}
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
