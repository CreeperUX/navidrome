import React, { useCallback, useEffect, useState } from 'react'
import PropTypes from 'prop-types'
import Button from '@material-ui/core/Button'
import Dialog from '@material-ui/core/Dialog'
import DialogActions from '@material-ui/core/DialogActions'
import DialogContent from '@material-ui/core/DialogContent'
import DialogTitle from '@material-ui/core/DialogTitle'
import TextField from '@material-ui/core/TextField'
import Typography from '@material-ui/core/Typography'
import { makeStyles } from '@material-ui/core/styles'
import { useNotify, useTranslate } from 'react-admin'
import {
  getTwoFactorStatus,
  getUserTwoFactor,
  issueEnrolmentCode,
  resetUserTwoFactor,
} from './api'

const useStyles = makeStyles((theme) => ({
  root: {
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(1),
    margin: theme.spacing(2, 0),
    maxWidth: '40em',
  },
  actions: { display: 'flex', gap: theme.spacing(1), flexWrap: 'wrap' },
  code: {
    fontFamily: 'monospace',
    fontSize: '1.4rem',
    letterSpacing: '0.06em',
    margin: theme.spacing(2, 0),
    userSelect: 'all',
  },
}))

const when = (date) => (date ? new Date(date).toLocaleString() : '')

// An admin's view of a user's two-step verification, on the user's page (Sonus). Where the server requires two-step
// verification, a user sets it up at the first sign-in with a one-time enrolment code besides the password: the admin
// makes one here (for a user just made, or one from before these codes) and hands it over with the password. A reset
// (a lost phone and no recovery codes) answers with a new one. Both take the admin's own verification code.
//
// It sits inside the user's form, so it has no form of its own: a form in a dialog would submit the page's.
export const UserTwoFactor = ({ record }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const classes = useStyles()
  const [status, setStatus] = useState(null)
  const [mine, setMine] = useState(null)
  const [dialog, setDialog] = useState(null)
  const [code, setCode] = useState('')
  const [busy, setBusy] = useState(false)
  const id = record?.id

  const reload = useCallback(() => {
    if (!id) return
    getUserTwoFactor(id)
      .then(setStatus)
      .catch(() => setStatus(null))
  }, [id])
  useEffect(reload, [reload])
  useEffect(() => {
    getTwoFactorStatus()
      .then(setMine)
      .catch(() => setMine(null))
  }, [])

  if (!id || !status || status.mode === 'off') return null
  const name = record.userName || record.name || ''
  // the admin's own code is asked for when the admin has two-step verification
  const needsCode = !!mine && mine.enabled && mine.mode !== 'off'

  const start = (action) => {
    setCode('')
    setDialog({ kind: 'ask', action })
  }

  const run = async () => {
    const { action } = dialog
    setBusy(true)
    try {
      const sent = needsCode ? code.trim() : undefined
      const res =
        action === 'reset'
          ? await resetUserTwoFactor(id, sent)
          : await issueEnrolmentCode(id, sent)
      if (res && res.enrolmentCode) {
        setDialog({ kind: 'made', ...res })
      } else {
        setDialog(null)
        notify('sonus.userTwoFactor.resetDone', 'info')
      }
      reload()
    } catch (e) {
      notify(
        e.twoFactor === 'code'
          ? 'sonus.twoFactor.invalidCode'
          : e.status === 404 || e.status === 405
            ? 'sonus.userTwoFactor.unsupported'
            : e.message,
        'warning',
      )
    } finally {
      setBusy(false)
    }
  }

  const copy = (text) =>
    navigator.clipboard
      ?.writeText(text)
      .then(() => notify('sonus.copied', 'info'))
      .catch(() => notify('sonus.copyFailed', 'warning'))

  const ready = !needsCode || code.trim().length >= 6
  return (
    <div className={classes.root}>
      <Typography variant="h6">{translate('sonus.twoFactor.title')}</Typography>
      <Typography variant="body2">
        {status.enabled
          ? translate('sonus.userTwoFactor.on', {
              date: when(status.enabledAt),
            })
          : translate('sonus.userTwoFactor.off')}
      </Typography>
      {!status.enabled && status.mode === 'required' && (
        <Typography variant="body2" color="textSecondary">
          {status.enrolmentCodeExpiresAt
            ? translate('sonus.userTwoFactor.waiting', {
                date: when(status.enrolmentCodeExpiresAt),
              })
            : translate('sonus.userTwoFactor.needsCode')}
        </Typography>
      )}
      <div className={classes.actions}>
        {!status.enabled && status.mode === 'required' && (
          <Button variant="outlined" onClick={() => start('issue')}>
            {translate('sonus.userTwoFactor.makeCode')}
          </Button>
        )}
        {status.enabled && (
          <Button variant="outlined" onClick={() => start('reset')}>
            {translate('sonus.userTwoFactor.reset')}
          </Button>
        )}
      </div>

      <Dialog
        open={dialog?.kind === 'ask'}
        onClose={() => setDialog(null)}
        maxWidth="xs"
        fullWidth
      >
        <DialogTitle>
          {translate(
            dialog?.action === 'reset'
              ? 'sonus.userTwoFactor.resetTitle'
              : 'sonus.userTwoFactor.makeTitle',
            { name },
          )}
        </DialogTitle>
        <DialogContent>
          <Typography variant="body2" paragraph>
            {translate(
              dialog?.action === 'reset'
                ? 'sonus.userTwoFactor.resetExplain'
                : 'sonus.userTwoFactor.makeExplain',
            )}
          </Typography>
          {needsCode && (
            <>
              <Typography variant="body2">
                {translate('sonus.twoFactor.confirmExplain')}
              </Typography>
              <TextField
                autoFocus
                label={translate('sonus.twoFactor.code')}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key !== 'Enter') return
                  e.preventDefault() // not the user's form
                  if (ready && !busy) run()
                }}
                inputProps={{
                  autoComplete: 'one-time-code',
                  'aria-label': translate('sonus.twoFactor.code'),
                }}
                fullWidth
              />
            </>
          )}
        </DialogContent>
        <DialogActions>
          <Button onClick={() => setDialog(null)}>
            {translate('ra.action.cancel')}
          </Button>
          <Button color="primary" disabled={busy || !ready} onClick={run}>
            {translate('sonus.twoFactor.confirm')}
          </Button>
        </DialogActions>
      </Dialog>

      <Dialog
        open={dialog?.kind === 'made'}
        onClose={() => setDialog(null)}
        maxWidth="sm"
        fullWidth
      >
        <DialogTitle>
          {translate('sonus.userTwoFactor.codeTitle', { name })}
        </DialogTitle>
        <DialogContent>
          <Typography variant="body2">
            {translate('sonus.userTwoFactor.codeExplain', {
              date: when(dialog?.expiresAt),
            })}
          </Typography>
          <div
            className={classes.code}
            aria-label={translate('sonus.twoFactor.enrolCode')}
          >
            {dialog?.enrolmentCode}
          </div>
          <Typography variant="body2" color="textSecondary">
            {translate('sonus.userTwoFactor.onlyOnce')}
          </Typography>
        </DialogContent>
        <DialogActions>
          <Button onClick={() => copy(dialog?.enrolmentCode || '')}>
            {translate('sonus.copy')}
          </Button>
          <Button color="primary" onClick={() => setDialog(null)}>
            {translate('sonus.done')}
          </Button>
        </DialogActions>
      </Dialog>
    </div>
  )
}

UserTwoFactor.propTypes = {
  record: PropTypes.object,
}
