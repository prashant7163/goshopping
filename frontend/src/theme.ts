import { createTheme } from '@mui/material/styles'

/**
 * The MUI theme. Deliberately small: the palette and shape are the things worth
 * fixing centrally, and everything else is left at MUI's defaults until a page
 * actually needs it.
 */
export const theme = createTheme({
  palette: {
    primary: { main: '#0f766e' },
    secondary: { main: '#b45309' },
  },
  shape: { borderRadius: 8 },
  typography: {
    button: {
      // MUI shouts by default; a shop reads better without it.
      textTransform: 'none',
      fontWeight: 600,
    },
  },
})
