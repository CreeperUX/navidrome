import React, { useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import Button from '@material-ui/core/Button'
import Card from '@material-ui/core/Card'
import CardContent from '@material-ui/core/CardContent'
import TextField from '@material-ui/core/TextField'
import Typography from '@material-ui/core/Typography'
import { makeStyles } from '@material-ui/core/styles'
import { Title, useTranslate } from 'react-admin'
import { authCall } from './api'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: '1em', maxWidth: 560 },
  body: { display: 'flex', flexDirection: 'column', gap: theme.spacing(2) },
  code: {
    fontFamily: 'monospace',
    fontSize: '2rem',
    letterSpacing: '0.15em',
    textAlign: 'center',
    padding: theme.spacing(1),
    border: `1px solid ${theme.palette.divider}`,
    borderRadius: 8,
  },
  actions: {
    display: 'flex',
    gap: theme.spacing(1),
    justifyContent: 'flex-end',
  },
  form: { display: 'flex', gap: theme.spacing(1), alignItems: 'flex-end' },
}))

// The browser sign-in's approval page (Sonus, server/sonus_device.go): an app (Sonus on some computer) asked to sign
// in; the signed-in user checks its code matches what the app shows and allows it, which hands the app an API key of
// its own, or refuses. #/device?code=ABCD-EFGH, as the app opens it. The route keys it on the query, so another app's
// link opened on this page starts over with that code.
const DeviceApproval = ({ location }) => {
  const translate = useTranslate()
  const classes = useStyles()
  const [code, setCode] = useState(
    new URLSearchParams(location?.search || '').get('code') || '',
  )
  // each code typed in is asked again, the same one too (it may have been mistyped the first time)
  const [asked, setAsked] = useState(0)
  const [typed, setTyped] = useState('')
  const [info, setInfo] = useState(null)
  const [state, setState] = useState(code ? 'loading' : 'ask')
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!code) return undefined
    let current = true // a later code's answer wins over this one's, however late it comes
    setState('loading')
    authCall('GET', `/device/${encodeURIComponent(code)}`)
      .then((i) => {
        if (!current) return
        setInfo(i)
        setState('ready')
      })
      .catch(() => current && setState('missing'))
    return () => {
      current = false
    }
  }, [code, asked])

  const decide = async (allow) => {
    setBusy(true)
    try {
      await authCall(
        'POST',
        `/device/${encodeURIComponent(info.userCode)}/${allow ? 'approve' : 'deny'}`,
      )
      setState(allow ? 'allowed' : 'denied')
    } catch {
      setState('missing')
    } finally {
      setBusy(false)
    }
  }

  const client = info?.client || ''
  return (
    <Card className={classes.root}>
      <Title title={'Sonus - ' + translate('sonus.device.title')} />
      <CardContent className={classes.body}>
        <Typography variant="h6">{translate('sonus.device.title')}</Typography>
        {state === 'ready' && (
          <>
            <Typography variant="body1">
              {translate('sonus.device.request', {
                client,
                device: info.device || '?',
                user: localStorage.getItem('username') || '',
              })}
            </Typography>
            <Typography variant="body2">
              {translate('sonus.device.compare')}
            </Typography>
            <div
              className={classes.code}
              aria-label={translate('sonus.device.codeLabel')}
            >
              {info.userCode}
            </div>
            <Typography variant="body2" color="textSecondary">
              {translate('sonus.device.whatItGets')}
            </Typography>
            <div className={classes.actions}>
              <Button onClick={() => decide(false)} disabled={busy}>
                {translate('sonus.device.deny')}
              </Button>
              <Button
                variant="contained"
                color="primary"
                onClick={() => decide(true)}
                disabled={busy}
              >
                {translate('sonus.device.allow', { client })}
              </Button>
            </div>
          </>
        )}
        {state === 'allowed' && (
          <Typography variant="body1" role="status">
            {translate('sonus.device.allowed', { client })}
          </Typography>
        )}
        {state === 'denied' && (
          <Typography variant="body1" role="status">
            {translate('sonus.device.denied', { client })}
          </Typography>
        )}
        {(state === 'missing' || state === 'ask') && (
          <>
            {state === 'missing' && (
              <Typography variant="body1" role="alert">
                {translate('sonus.device.missing')}
              </Typography>
            )}
            <Typography variant="body2">
              {translate('sonus.device.typeCode')}
            </Typography>
            <form
              className={classes.form}
              onSubmit={(e) => {
                e.preventDefault()
                setCode(typed.trim())
                setAsked((n) => n + 1)
              }}
              noValidate
            >
              <TextField
                label={translate('sonus.device.codeLabel')}
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
                inputProps={{ autoCapitalize: 'characters' }}
              />
              <Button
                type="submit"
                variant="contained"
                color="primary"
                disabled={!typed.trim()}
              >
                {translate('sonus.device.continue')}
              </Button>
            </form>
          </>
        )}
      </CardContent>
    </Card>
  )
}

DeviceApproval.propTypes = {
  location: PropTypes.object,
}

export default DeviceApproval
