import React, { useState } from 'react'
import { Button, Checkbox, FormControlLabel, Dialog, DialogActions, DialogContent, DialogContentText, DialogTitle } from '@material-ui/core'
import WarningIcon from '@material-ui/icons/Warning'
import env from "react-dotenv"

const BRIDGE_TFT_ADDRESS = env.BRIDGE_TFT_ADDRESS

export function Deposit({ open, handleClose }) {
  const [checked, setChecked] = useState(false)

  return (
    <div>
      <Dialog
        open={open}
        onClose={handleClose}
        aria-labelledby="alert-dialog-title"
        aria-describedby="alert-dialog-description"
        fullScreen={true}
      >
        <DialogTitle id="alert-dialog-title">{"Swap Stellar TFT for Tfchain TFT"}</DialogTitle>
        <DialogContent>
          <DialogContentText style={{ margin: 'auto', textAlign: 'center',  width: '50%', display: 'flex', flexDirection: 'column' }}>
            <WarningIcon style={{ alignSelf: 'center', fontSize: 40, color : 'orange' }}/>
            If you want to swap your Stellar TFT to Tfchain TFT you can transfer any amount to the destination address. 
            Important Note: Please check what memo text you need to send. <b style={{ color: 'red', marginTop: 5 }}>Failure to do so will result in unrecoverable loss of funds!</b>
          </DialogContentText>

          <FormControlLabel
            style={{ margin: 'auto', marginTop: 20, width: '60%', display: 'flex', justifyContent: 'center' }}
            control={
              <Checkbox value={checked} onChange={(e) => setChecked(e.target.checked)} />
            }
            label="I understand that I need to include a memo text for every swap transaction, I am responsible for the loss of funds consequence otherwise."
          />

          {checked && (
            <>
              <DialogContentText style={{ margin: 'auto', textAlign: 'center', display: 'flex', flexDirection: 'column', marginTop: 40 }}>
                <span><b>Enter the following information manually:</b></span>
                <span>
                Following objects can be deposited to: [Farm, Node, Twin, Entity]
                </span>
                <span>
                To deposit to any of these objects, a memo text in format `object_objectID` must be passed on the deposit to the bridge wallet. Example: `twin_1`. 

                </span>
                To deposit to a TF Grid object, this object **must** exists. If the object is not found on chain, a refund is issued.
                <span style={{ marginTop: 20 }}>Destination Stellar Address: <b>{BRIDGE_TFT_ADDRESS}</b></span>
                <span><b>Memo text: "object_objectID"</b></span>
              </DialogContentText>
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button style={{ width: 200, height: 50 }} variant='contained' onClick={handleClose} color="primary">
            Close
          </Button>
        </DialogActions>
      </Dialog>
    </div>
  )
}
