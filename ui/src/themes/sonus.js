import stylesheet from './sonus.css.js'

// Sonus: the colours of the Sonus Windows player (CreeperUX design tokens, dark mode) on the Dark theme's layout. Ink
// backgrounds, warm white text, and amber for what is active.
const amber = { light: '#ffc522', main: '#f8b700', dark: '#e5a800' }
const ink = '#111416' // text on amber
const background = '#040607'
const panel = '#151b1f'
const raised = '#20272c'
const text = '#f4f1eb'
const muted = '#a9b0b3'
const line = 'rgba(239, 235, 229, 0.14)'

export default {
  themeName: 'Sonus',
  palette: {
    primary: { ...amber, contrastText: ink },
    secondary: { ...amber, contrastText: ink },
    background: {
      default: background,
      paper: panel,
    },
    text: {
      primary: text,
      secondary: muted,
    },
    divider: line,
    type: 'dark',
  },
  overrides: {
    MuiFormGroup: {
      root: {
        color: text,
      },
    },
    MuiButton: {
      textPrimary: {
        color: amber.main,
      },
    },
    MuiAppBar: {
      colorSecondary: {
        backgroundColor: '#0b0f11',
        color: text,
      },
    },
    MuiDrawer: {
      paper: {
        backgroundColor: background,
      },
    },
    MuiTableCell: {
      root: {
        borderBottom: `1px solid ${line}`,
      },
    },
    MuiTableRow: {
      hover: {
        '&:hover': {
          backgroundColor: `${raised} !important`,
        },
      },
    },
    NDLogin: {
      main: {
        boxShadow: 'inset 0 0 0 2000px rgba(4, 6, 7, .6)',
      },
      systemNameLink: {
        color: amber.main,
      },
      icon: {},
      welcome: {
        color: text,
      },
      card: {
        minWidth: 300,
        backgroundColor: 'rgba(21, 27, 31, .94)',
        border: `1px solid ${line}`,
      },
      avatar: {},
      button: {
        boxShadow: '3px 3px 5px #000000a3',
      },
    },
    NDMobileArtistDetails: {
      bgContainer: {
        background: `linear-gradient(to bottom, rgba(21 27 31 / 72%), ${background})!important`,
      },
    },
  },
  player: {
    theme: 'dark',
    stylesheet,
  },
}
