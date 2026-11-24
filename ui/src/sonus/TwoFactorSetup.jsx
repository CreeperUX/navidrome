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
// authenticator being replaced. needsEnrolmentCode: the server wants the one-time enrolment code an admin handed over
// with the password, which is asked for first.
export const TwoFactorSetup = ({
  token,
  currentCode,
  needsEnrolmentCode,
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
  // the enrolment code as typed, and as sent: null until it is entered (none is needed: sent at once, as nothing)
  const [typed, setTyped] = useState('')
  const [enrolment, setEnrolment] = useState(needsEnrolmentCode ? null : '')

  // the server refused the enrolment code: back to asking for it
  const askAgain = (e) => {
    setSetup(null)
    setEnrolment(null)
    setError(
      e.status === 429
        ? translate('sonus.twoFactor.tooMany')
        : translate('sonus.twoFactor.invalidEnrolment'),
    )
  }

  useEffect(() => {
    if (enrolment === null) return
    beginSetup(token, currentCode, enrolment || undefined)
      .then(setSetup)
      .catch((e) =>
        needsEnrolmentCode && (e.twoFactor === 'enrolment' || e.status === 429)
          ? askAgain(e)
          : setError(
              e.twoFactor === 'code'
                ? translate('sonus.twoFactor.invalidCode')
                : e.message,
            ),
      )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, currentCode, enrolment, translate])

  const confirm = async (e) => {
    e.preventDefault()
    setBusy(true)
    setError('')
    try {
      setResult(await confirmSetup(token, code, enrolment || undefined))
    } catch (err) {
      if (err.twoFactor === 'enrolment') {
        askAgain(err)
      } else {
        setError(
          err.status === 401
            ? translate('sonus.twoFactor.invalidCode')
            : err.message,
        )
      }
    } finally {
      setBusy(false)
    }
  }

  if (enrolment === null) {
    return (
      <form
        className={classes.root}
        onSubmit={(e) => {
          e.preventDefault()
          setError('')
          setEnrolment(typed.trim())
        }}
        noValidate
      >
        {intro && <Typography variant="body2">{intro}</Typography>}
        <Typography variant="h6">
          {translate('sonus.twoFactor.enrolTitle')}
        </Typography>
        <Typography variant="body2">
          {translate('sonus.twoFactor.enrolExplain')}
        </Typography>
        <TextField
          autoFocus
          label={translate('sonus.twoFactor.enrolCode')}
          value={typed}
          onChange={(e) => setTyped(e.target.value)}
          inputProps={{
            autoComplete: 'off',
            autoCapitalize: 'none',
            spellCheck: false,
            'aria-label': translate('sonus.twoFactor.enrolCode'),
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
            <Button onClick={onCancel}>{translate('ra.action.cancel')}</Button>
          )}
          <Button
            type="submit"
            variant="contained"
            color="primary"
            disabled={typed.trim().length < 12}
          >
            {translate('sonus.device.continue')}
          </Button>
        </div>
      </form>
    )
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
  needsEnrolmentCode: PropTypes.bool,
  intro: PropTypes.string,
  onDone: PropTypes.func.isRequired,
  onCancel: PropTypes.func,
}
