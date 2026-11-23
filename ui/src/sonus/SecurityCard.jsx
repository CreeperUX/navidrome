import React, { useCallback, useEffect, useState } from 'react'
import Button from '@material-ui/core/Button'
import Card from '@material-ui/core/Card'
import CardContent from '@material-ui/core/CardContent'
import Dialog from '@material-ui/core/Dialog'
import DialogActions from '@material-ui/core/DialogActions'
import DialogContent from '@material-ui/core/DialogContent'
import DialogTitle from '@material-ui/core/DialogTitle'
import Divider from '@material-ui/core/Divider'
import TextField from '@material-ui/core/TextField'
import Typography from '@material-ui/core/Typography'
import { makeStyles } from '@material-ui/core/styles'
import { useNotify, useTranslate } from 'react-admin'
import {
  createAppPassword,
  deleteAppPassword,
  disableTwoFactor,
  getTwoFactorStatus,
  listAppPasswords,
  newRecoveryCodes,
} from './api'
import { RecoveryCodes } from './RecoveryCodes'
import { TwoFactorSetup } from './TwoFactorSetup'

const useStyles = makeStyles((theme) => ({
  root: { marginTop: '1em' },
  section: { display: 'flex', flexDirection: 'column', gap: theme.spacing(1) },
  actions: { display: 'flex', gap: theme.spacing(1), flexWrap: 'wrap' },
  divider: { margin: theme.spacing(3, 0) },
  app: {
    display: 'flex',
    alignItems: 'center',
    gap: theme.spacing(2),
    padding: theme.spacing(1, 0),
    borderBottom: `1px solid ${theme.palette.divider}`,
  },
  appText: { flex: 1, minWidth: 0 },
  create: {
    display: 'flex',
    gap: theme.spacing(1),
    alignItems: 'flex-end',
    maxWidth: 480,
  },
  password: {
    fontFamily: 'monospace',
    fontSize: '1.4rem',
    letterSpacing: '0.06em',
    margin: theme.spacing(2, 0),
    userSelect: 'all',
  },
}))

const when = (date) => (date ? new Date(date).toLocaleString() : '')

// Sonus's part of the personal settings: two-step verification (set it up, new recovery codes, another authenticator,
// off when the server allows it) and app passwords (made for each client, shown once, revoked here).
export const SecurityCard = () => {
  const translate = useTranslate()
  const notify = useNotify()
  const classes = useStyles()
  const [status, setStatus] = useState(null)
  const [apps, setApps] = useState([])
  const [name, setName] = useState('')
  const [dialog, setDialog] = useState(null)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)

  const reload = useCallback(() => {
    getTwoFactorStatus()
      .then(setStatus)
      .catch(() => setStatus(null))
    listAppPasswords()
      .then(setApps)
      .catch(() => setApps([]))
  }, [])
  useEffect(reload, [reload])

  const fail = (e) =>
    notify(
      e.twoFactor === 'code' ? 'sonus.twoFactor.invalidCode' : e.message,
      'warning',
    )

  const askCode = (action) => {
    setCode('')
    setDialog({ kind: 'code', action })
  }
  const withCode = async (e) => {
    e.preventDefault()
    const { action } = dialog
    if (action === 'replace') {
      setDialog({ kind: 'setup', currentCode: code.trim() })
      return
    }
    setBusy(true)
    try {
      if (action === 'recovery') {
        const res = await newRecoveryCodes(code.trim())
        setDialog({ kind: 'recovery', codes: res.recoveryCodes })
      } else if (action === 'disable') {
        await disableTwoFactor(code.trim())
        setDialog(null)
        notify('sonus.twoFactor.turnedOff', 'info')
      }
      reload()
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const setupDone = (result) => {
    // the session the setup answered with: no password-derived Subsonic token any more (authProvider)
    if (result.token) localStorage.setItem('token', result.token)
    if (!result.subsonicToken) {
      localStorage.removeItem('subsonic-salt')
      localStorage.removeItem('subsonic-token')
    }
    setDialog(null)
    notify('sonus.twoFactor.turnedOn', 'info')
    reload()
  }

  const create = async (e) => {
    e.preventDefault()
    setBusy(true)
    try {
      const res = await createAppPassword(name.trim())
      setDialog({
        kind: 'created',
        name: res.appPassword.name,
        password: res.password,
      })
      setName('')
      reload()
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const revoke = async (app) => {
    setBusy(true)
    try {
      await deleteAppPassword(app.id)
      setDialog(null)
      notify('sonus.appPasswords.revoked', 'info')
      reload()
    } catch (err) {
      fail(err)
    } finally {
      setBusy(false)
    }
  }

  const copy = (text) =>
    navigator.clipboard
      ?.writeText(text)
      .then(() => notify('sonus.copied', 'info'))
      .catch(() => notify('sonus.copyFailed', 'warning'))

  return (
    <Card className={classes.root}>
      <CardContent>
        <div className={classes.section}>
          <Typography variant="h6">
            {translate('sonus.twoFactor.title')}
          </Typography>
          {status && (
            <>
              <Typography variant="body2">
                {status.enabled
                  ? translate('sonus.twoFactor.isOn', {
                      date: when(status.enabledAt),
                    })
                  : translate(
                      status.mode === 'off'
                        ? 'sonus.twoFactor.serverOff'
                        : 'sonus.twoFactor.isOff',
                    )}
              </Typography>
              {status.enabled && (
                <Typography variant="body2" color="textSecondary">
                  {translate('sonus.twoFactor.recoveryLeft', {
                    smart_count: status.recoveryCodesLeft,
                  })}
                </Typography>
              )}
              <div className={classes.actions}>
                {!status.enabled && status.mode !== 'off' && (
                  <Button
                    variant="contained"
                    color="primary"
                    onClick={() => setDialog({ kind: 'setup' })}
                  >
                    {translate('sonus.twoFactor.turnOn')}
                  </Button>
                )}
                {status.enabled && (
                  <>
                    <Button
                      variant="outlined"
                      onClick={() => askCode('recovery')}
                    >
                      {translate('sonus.twoFactor.newRecovery')}
                    </Button>
                    <Button
                      variant="outlined"
                      onClick={() => askCode('replace')}
                    >
                      {translate('sonus.twoFactor.replace')}
                    </Button>
                    {status.mode === 'optional' && (
                      <Button
                        variant="outlined"
                        onClick={() => askCode('disable')}
                      >
                        {translate('sonus.twoFactor.turnOff')}
                      </Button>
                    )}
                  </>
                )}
              </div>
            </>
          )}
        </div>

        <Divider className={classes.divider} />

        <div className={classes.section}>
          <Typography variant="h6">
            {translate('sonus.appPasswords.title')}
          </Typography>
          <Typography variant="body2">
            {translate('sonus.appPasswords.explain')}
          </Typography>
          <div>
            {apps.length === 0 && (
              <Typography variant="body2" color="textSecondary">
                {translate('sonus.appPasswords.none')}
              </Typography>
            )}
            {apps.map((app) => (
              <div key={app.id} className={classes.app}>
                <div className={classes.appText}>
                  <Typography variant="body1">{app.name}</Typography>
                  <Typography variant="body2" color="textSecondary">
                    {translate('sonus.appPasswords.created', {
                      date: when(app.createdAt),
                    })}
                    {' · '}
                    {app.lastUsedAt
                      ? translate('sonus.appPasswords.lastUsed', {
                          date: when(app.lastUsedAt),
                          client: app.lastUsedBy || '?',
                        })
                      : translate('sonus.appPasswords.neverUsed')}
                  </Typography>
                </div>
                <Button
                  size="small"
                  onClick={() => setDialog({ kind: 'revoke', app })}
                  aria-label={`${translate('sonus.appPasswords.revoke')} ${app.name}`}
                >
                  {translate('sonus.appPasswords.revoke')}
                </Button>
              </div>
            ))}
          </div>
          <form className={classes.create} onSubmit={create} noValidate>
            <TextField
              label={translate('sonus.appPasswords.name')}
              placeholder={translate('sonus.appPasswords.namePlaceholder')}
              value={name}
              onChange={(e) => setName(e.target.value)}
              inputProps={{ maxLength: 64 }}
              fullWidth
            />
            <Button
              type="submit"
              variant="contained"
              color="primary"
              disabled={busy || !name.trim()}
            >
              {translate('sonus.appPasswords.create')}
            </Button>
          </form>
        </div>
      </CardContent>

      <Dialog
        open={dialog?.kind === 'code'}
        onClose={() => setDialog(null)}
        maxWidth="xs"
        fullWidth
      >
        <form onSubmit={withCode} noValidate>
          <DialogTitle>{translate('sonus.twoFactor.confirmTitle')}</DialogTitle>
          <DialogContent>
            <Typography variant="body2">
              {translate('sonus.twoFactor.confirmExplain')}
            </Typography>
            <TextField
              autoFocus
              label={translate('sonus.twoFactor.code')}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              inputProps={{
                autoComplete: 'one-time-code',
                'aria-label': translate('sonus.twoFactor.code'),
              }}
              fullWidth
            />
          </DialogContent>
          <DialogActions>
            <Button onClick={() => setDialog(null)}>
              {translate('ra.action.cancel')}
            </Button>
            <Button
              type="submit"
              color="primary"
              disabled={busy || code.trim().length < 6}
            >
              {translate('sonus.twoFactor.confirm')}
            </Button>
          </DialogActions>
        </form>
      </Dialog>

      <Dialog
        open={dialog?.kind === 'setup'}
        onClose={() => setDialog(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogContent>
          {dialog?.kind === 'setup' && (
            <TwoFactorSetup
              token={localStorage.getItem('token')}
              currentCode={dialog.currentCode}
              onDone={setupDone}
              onCancel={() => setDialog(null)}
            />
          )}
        </DialogContent>
      </Dialog>

      <Dialog
        open={dialog?.kind === 'recovery'}
        onClose={() => setDialog(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>{translate('sonus.twoFactor.recoveryTitle')}</DialogTitle>
        <DialogContent>
          {dialog?.kind === 'recovery' && (
            <RecoveryCodes codes={dialog.codes} />
          )}
        </DialogContent>
        <DialogActions>
          <Button color="primary" onClick={() => setDialog(null)}>
            {translate('sonus.twoFactor.savedContinue')}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={dialog?.kind === 'created'}
        onClose={() => setDialog(null)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>
          {translate('sonus.appPasswords.createdTitle', {
            name: dialog?.name || '',
          })}
        </DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            {translate('sonus.appPasswords.createdExplain')}
          </Typography>
          <div
            className={classes.password}
            aria-label={translate('sonus.appPasswords.title')}
          >
            {dialog?.password}
          </div>
          <Typography variant="body2" color="textSecondary">
            {translate('sonus.appPasswords.onlyOnce')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => copy(dialog?.password || '')}>
            {translate('sonus.copy')}
          </Button>
          <Button color="primary" onClick={() => setDialog(null)}>
            {translate('sonus.done')}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={dialog?.kind === 'revoke'}
        onClose={() => setDialog(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>
          {translate('sonus.appPasswords.revokeTitle', {
            name: dialog?.app?.name || '',
          })}
        </DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            {translate('sonus.appPasswords.revokeExplain')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialog(null)}>
            {translate('ra.action.cancel')}
          </Button>
          <Button
            color="primary"
            disabled={busy}
            onClick={() => revoke(dialog.app)}
          >
            {translate('sonus.appPasswords.revoke')}
          </Button>
        </DialogActions>
      </Dialog>
    </Card>
  )
}
