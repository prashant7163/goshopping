import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { Provider } from 'react-redux'
import { BrowserRouter } from 'react-router-dom'
import CssBaseline from '@mui/material/CssBaseline'
import { StyledEngineProvider, ThemeProvider } from '@mui/material/styles'

import App from './App.tsx'
import { store } from './app/store.ts'
import { theme } from './theme.ts'
import './index.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Provider store={store}>
      {/*
        enableCssLayer is what puts every rule MUI emits inside `@layer mui`,
        which the layer order in index.html ranks between Tailwind's reset and
        its utilities. It is set at the root, on day one, because retrofitting
        it means re-auditing every override written in the meantime (ADR-031).
      */}
      <StyledEngineProvider enableCssLayer>
        <ThemeProvider theme={theme}>
          <CssBaseline />
          <BrowserRouter>
            <App />
          </BrowserRouter>
        </ThemeProvider>
      </StyledEngineProvider>
    </Provider>
  </StrictMode>,
)
