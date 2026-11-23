import React from 'react'
import PropTypes from 'prop-types'
import Button from '@material-ui/core/Button'
import Typography from '@material-ui/core/Typography'
import { makeStyles } from '@material-ui/core/styles'
import { useNotify, useTranslate } from 'react-admin'

const useStyles = makeStyles((theme) => ({
  codes: {
    display: 'grid',
    gridTemplateColumns: 'repeat(2, max-content)',
    gap: '0.4em 2em',
    margin: '1em 0',
    fontFamily: 'monospace',
    fontSize: '1.05rem',
    letterSpacing: '0.05em',
  },
  actions: { display: 'flex', gap: theme.spacing(1), flexWrap: 'wrap' },
}))

// The recovery codes, shown once: each signs in once in place of an authenticator code (a lost phone).
export const RecoveryCodes = ({ codes, account }) => {
  const translate = useTranslate()
  const notify = useNotify()
  const classes = useStyles()
  const text = `Sonus ${account ? `(${account}) ` : ''}${translate('sonus.twoFactor.recoveryTitle')}\n\n${codes.join('\n')}\n`

  const copy = () =>
    navigator.clipboard
      ?.writeText(text)
      .then(() => notify('sonus.copied', 'info'))
      .catch(() => notify('sonus.copyFailed', 'warning'))
  const download = () => {
    const link = document.createElement('a')
    link.href = URL.createObjectURL(new Blob([text], { type: 'text/plain' }))
    link.download = 'sonus-recovery-codes.txt'
    link.click()
    URL.revokeObjectURL(link.href)
  }

  return (
    <div>
      <Typography variant="body2">
        {translate('sonus.twoFactor.recoveryExplain')}
      </Typography>
      <div
        className={classes.codes}
        aria-label={translate('sonus.twoFactor.recoveryTitle')}
      >
        {codes.map((c) => (
          <span key={c}>{c}</span>
        ))}
      </div>
      <div className={classes.actions}>
        <Button variant="outlined" size="small" onClick={copy}>
          {translate('sonus.copy')}
        </Button>
        <Button variant="outlined" size="small" onClick={download}>
          {translate('sonus.download')}
        </Button>
      </div>
    </div>
  )
}

RecoveryCodes.propTypes = {
  codes: PropTypes.arrayOf(PropTypes.string).isRequired,
  account: PropTypes.string,
}
