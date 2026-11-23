import React, { useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import Button from '@material-ui/core/Button'
import CircularProgress from '@material-ui/core/CircularProgress'
import TextField from '@material-ui/core/TextField'
import Typography from '@material-ui/core/Typography'
import { makeStyles } from '@material-ui/core/styles'
import { useTranslate } from 'react-admin'
import { beginSetup, confirmSetup } from './api'
import { RecoveryCodes } from './RecoveryCodes'

const useStyles = makeStyles((theme) => ({
  root: { display: 'flex', flexDirection: 'column', gap: theme.spacing(1.5) },
  qr: {
    alignSelf: 'center',
    width: 196,
    height: 196,
    imageRendering: 'pixelated',
    background: '#fff',
    padding: 8,
    borderRadius: 8,
  },
  secret: {
    fontFamily: 'monospace',
    letterSpacing: '0.08em',
    wordBreak: 'break-all',
    userSelect: 'all',
  },
  actions: {
    display: 'flex',
    gap: theme.spacing(1),
    justifyContent: 'flex-end',
  },
  error: { color: theme.palette.error.main },
}))

// groups of four, as authenticator apps show a secret typed in by hand
const grouped = (secret) => secret.replace(/(.{4})/g, '$1 ').trim()

// Setting up an authenticator: its QR code (or the secret, typed in), then a code from it to confirm, then the recovery
// codes to keep. token is the setup token from the sign-in, or the session's; currentCode a code from the
// authenticator being replaced.
export const TwoFactorSetup = ({
  token,
  currentCode,
  intro,
  onDone,
  onCancel,
}) => {
  const translate = useTranslate()
  const classes = useStyles()
  const [setup, setSetup] = useState(null)
  const [code, setCode] = useState('')
  const [error, setError] = useState('')
  const [busy, setBusy] = useState(false)
  const [result, setResult] = useState(null)

  useEffect(() => {
    beginSetup(token, currentCode)
      .then(setSetup)
      .catch((e) =>
        setError(
          e.twoFactor === 'code'
            ? translate('sonus.twoFactor.invalidCode')
            : e.message,
        ),
      )
  }, [token, currentCode, translate])

  const confirm = async (e) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      setResult(await confirmSetup(token, code))
    } catch (err) {
      setError(
        err.status === 401
          ? translate('sonus.twoFactor.invalidCode')
          : err.message,
      )
    } finally {
      setBusy(false)
    }
  }

  if (result) {
    return (
      <div className={classes.root}>
        <Typography variant="h6">
          {translate('sonus.twoFactor.doneTitle')}
        </Typography>
        <RecoveryCodes codes={result.recoveryCodes} account={setup?.account} />
        <div className={classes.actions}>
          <Button
            variant="contained"
            color="primary"
            onClick={() => onDone(result)}
          >
            {translate('sonus.twoFactor.savedContinue')}
          </Button>
        </div>
      </div>
    )
  }

  return (
    <form className={classes.root} onSubmit={confirm} noValidate>
      {intro && <Typography variant="body2">{intro}</Typography>}
      <Typography variant="h6">
        {translate('sonus.twoFactor.setupTitle')}
      </Typography>
      <Typography variant="body2">
        {translate('sonus.twoFactor.setupScan')}
      </Typography>
      {setup ? (
        <>
          {setup.qr && (
            <img
              className={classes.qr}
              src={setup.qr}
              alt={translate('sonus.twoFactor.qrAlt')}
            />
          )}
          <Typography variant="body2">
            {translate('sonus.twoFactor.setupKey')}{' '}
            <span className={classes.secret}>{grouped(setup.secret)}</span>
          </Typography>
        </>
      ) : (
        !error && <CircularProgress size={28} />
      )}
      <Typography variant="body2">
        {translate('sonus.twoFactor.setupCode')}
      </Typography>
      <TextField
        autoFocus
        label={translate('sonus.twoFactor.code')}
        value={code}
        onChange={(e) => setCode(e.target.value)}
        disabled={!setup || busy}
        inputProps={{
          inputMode: 'numeric',
          autoComplete: 'one-time-code',
          'aria-label': translate('sonus.twoFactor.code'),
        }}
        fullWidth
      />
      {error && (
        <Typography variant="body2" className={classes.error} role="alert">
          {error}
        </Typography>
      )}
      <div className={classes.actions}>
        {onCancel && (
          <Button onClick={onCancel} disabled={busy}>
            {translate('ra.action.cancel')}
          </Button>
        )}
        <Button
          type="submit"
          variant="contained"
          color="primary"
          disabled={!setup || busy || code.trim().length < 6}
        >
          {busy && <CircularProgress size={20} thickness={2} />}
          {translate('sonus.twoFactor.confirm')}
        </Button>
      </div>
    </form>
  )
}

TwoFactorSetup.propTypes = {
  token: PropTypes.string,
  currentCode: PropTypes.string,
  intro: PropTypes.string,
  onDone: PropTypes.func.isRequired,
  onCancel: PropTypes.func,
}
