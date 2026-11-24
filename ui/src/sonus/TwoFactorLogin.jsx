import React, { useState } from 'react'
import PropTypes from 'prop-types'
import Button from '@material-ui/core/Button'
import Card from '@material-ui/core/Card'
import CircularProgress from '@material-ui/core/CircularProgress'
import TextField from '@material-ui/core/TextField'
import Typography from '@material-ui/core/Typography'
import { makeStyles } from '@material-ui/core/styles'
import { useTranslate } from 'react-admin'
import Notification from '../layout/Notification'
import { TwoFactorSetup } from './TwoFactorSetup'

const useStyles = makeStyles((theme) => ({
  card: { maxWidth: 380, width: 'calc(100vw - 32px)' },
  body: {
    padding: '1.5em 1.5em 1em',
    display: 'flex',
    flexDirection: 'column',
    gap: theme.spacing(1.5),
  },
  actions: {
    display: 'flex',
    gap: theme.spacing(1),
    justifyContent: 'flex-end',
  },
}))

// The sign-in's second step (Sonus): the authenticator code, or setting one up first. loginClasses are the sign-in
// page's own (NDLogin), so this sits where the password form was, in the same theme.
export const TwoFactorLogin = ({
  twoFactor,
  loading,
  loginClasses,
  onCode,
  onSetupDone,
  onCancel,
}) => {
  const translate = useTranslate()
  const classes = useStyles()
  const [code, setCode] = useState('')

  return (
    <div className={loginClasses.main}>
      <Card className={`${loginClasses.card} ${classes.card}`}>
        <div className={classes.body}>
          {twoFactor.step === 'setup' ? (
            <TwoFactorSetup
              token={twoFactor.token}
              needsEnrolmentCode={twoFactor.enrolment}
              intro={translate('sonus.twoFactor.requiredExplain')}
              onDone={onSetupDone}
              onCancel={onCancel}
            />
          ) : (
            <form
              onSubmit={(e) => {
                e.preventDefault()
                onCode(code.trim())
              }}
              noValidate
              className={classes.body}
              style={{ padding: 0 }}
            >
              <Typography variant="h6">
                {translate('sonus.twoFactor.signInTitle')}
              </Typography>
              <Typography variant="body2">
                {translate('sonus.twoFactor.enterCode')}
              </Typography>
              <TextField
                autoFocus
                label={translate('sonus.twoFactor.code')}
                value={code}
                onChange={(e) => setCode(e.target.value)}
                disabled={loading}
                inputProps={{
                  autoComplete: 'one-time-code',
                  autoCapitalize: 'none',
                  'aria-label': translate('sonus.twoFactor.code'),
                }}
                fullWidth
              />
              <Typography variant="caption" color="textSecondary">
                {translate('sonus.twoFactor.useRecovery')}
              </Typography>
              <div className={classes.actions}>
                <Button onClick={onCancel} disabled={loading}>
                  {translate('ra.action.back')}
                </Button>
                <Button
                  type="submit"
                  variant="contained"
                  color="primary"
                  disabled={loading || code.trim().length < 6}
                >
                  {loading && <CircularProgress size={20} thickness={2} />}
                  {translate('ra.auth.sign_in')}
                </Button>
              </div>
            </form>
          )}
        </div>
      </Card>
      <Notification />
    </div>
  )
}

TwoFactorLogin.propTypes = {
  twoFactor: PropTypes.shape({
    step: PropTypes.oneOf(['code', 'setup']).isRequired,
    token: PropTypes.string,
    enrolment: PropTypes.bool,
  }).isRequired,
  loading: PropTypes.bool,
  loginClasses: PropTypes.object.isRequired,
  onCode: PropTypes.func.isRequired,
  onSetupDone: PropTypes.func.isRequired,
  onCancel: PropTypes.func.isRequired,
}
