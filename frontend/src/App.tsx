import { useLayoutEffect, useRef, useState } from 'react'
import { useLocation } from 'react-router-dom'
import Alert from '@mui/material/Alert'
import Button from '@mui/material/Button'
import Container from '@mui/material/Container'
import Paper from '@mui/material/Paper'
import Stack from '@mui/material/Stack'
import Typography from '@mui/material/Typography'

/**
 * Phase 4 is scaffolding, so what it has to show is that the pieces fit
 * together — above all that a Tailwind utility beats a MUI default, which is
 * the arrangement ADR-005 sets up and the one thing here that fails silently.
 *
 * The checks below run in the browser against real computed styles rather than
 * against what the CSS says, because Emotion injects MUI's rules at runtime and
 * the built stylesheet does not show them at all.
 *
 * Phase 5 replaces this component with the actual pages.
 */

type Check = {
  name: string
  passed: boolean
  detail: string
}

/** Rules from every stylesheet the document has, same-origin ones included. */
function allRules(): CSSRule[] {
  return Array.from(document.styleSheets).flatMap((sheet) => {
    try {
      return Array.from(sheet.cssRules)
    } catch {
      // A cross-origin stylesheet throws on access. None of ours are.
      return []
    }
  })
}

function useStackChecks() {
  const plain = useRef<HTMLButtonElement>(null)
  const overridden = useRef<HTMLButtonElement>(null)
  const [checks, setChecks] = useState<Check[] | null>(null)

  useLayoutEffect(() => {
    if (!plain.current || !overridden.current) {
      return
    }

    // The order layers are established in, across every stylesheet. Tailwind
    // rewrites the single `@layer a, b, c;` statement in index.css into
    // separate declarations, so this reads positions rather than looking for
    // that statement verbatim — which is what a build step is free to change.
    const layers: string[] = []
    let layeredMuiBlocks = 0

    for (const rule of allRules()) {
      const declaration = /^@layer\s+([^{;]+)/.exec(rule.cssText)
      if (!declaration) {
        continue
      }
      for (const name of declaration[1].split(',').map((part) => part.trim())) {
        if (name && !layers.includes(name)) {
          layers.push(name)
        }
      }
      if (/^@layer\s+mui\s*\{/.test(rule.cssText)) {
        layeredMuiBlocks += 1
      }
    }

    const muiLayer = layers.indexOf('mui')
    const baseLayer = layers.indexOf('base')
    const utilitiesLayer = layers.indexOf('utilities')

    // Two identical MUI buttons, one carrying Tailwind utilities. Comparing
    // them against each other avoids asserting on a literal colour, which
    // Tailwind v4 emits as oklch and the browser may report either way.
    const styled = getComputedStyle(plain.current)
    const over = getComputedStyle(overridden.current)

    // Transparent with no padding is what Tailwind's preflight leaves behind.
    const strippedByReset =
      /rgba\(0, 0, 0, 0\)|transparent/.test(styled.backgroundColor) ||
      styled.paddingLeft === '0px'

    setChecks([
      {
        name: 'The mui layer sits above Tailwind\u2019s reset and below its utilities',
        passed: baseLayer >= 0 && muiLayer > baseLayer && utilitiesLayer > muiLayer,
        detail: layers.join(' < ') || 'no layers found',
      },
      {
        name: 'MUI rules are emitted inside @layer mui',
        passed: layeredMuiBlocks > 0,
        detail: `${layeredMuiBlocks} layered rule block(s) from Emotion`,
      },
      {
        // The check that matters most, and the easiest one to leave out. Rank
        // `mui` below the reset instead of above it and every other check here
        // still passes — because preflight has already stripped the button of
        // the background and padding they are comparing against.
        name: 'MUI\u2019s own button styling survives Tailwind\u2019s reset',
        passed: !strippedByReset,
        detail: `background ${styled.backgroundColor}, padding-left ${styled.paddingLeft}`,
      },
      {
        name: 'Tailwind background beats the MUI palette',
        passed: over.backgroundColor !== styled.backgroundColor,
        detail: `MUI ${styled.backgroundColor} → Tailwind ${over.backgroundColor}`,
      },
      {
        name: 'Tailwind padding beats the MUI Button padding',
        passed: over.paddingLeft !== styled.paddingLeft,
        detail: `MUI ${styled.paddingLeft} → Tailwind ${over.paddingLeft}`,
      },
    ])
  }, [])

  return { plain, overridden, checks }
}

export default function App() {
  const { plain, overridden, checks } = useStackChecks()
  const location = useLocation()
  const failed = checks?.filter((check) => !check.passed) ?? []

  return (
    <Container maxWidth="md" className="py-12">
      <Stack spacing={4}>
        <div>
          {/* MUI v9 dropped the system-prop shorthands (fontWeight, alignItems
              and friends) in favour of sx. Tailwind covers them here, which is
              also a second demonstration that the layering works. */}
          <Typography variant="h4" component="h1" className="font-bold">
            GoShopping
          </Typography>
          <Typography variant="body1" color="text.secondary">
            Phase 4 scaffold — Vite, React, TypeScript, Tailwind v4, MUI v9,
            React Router. Pages land in Phase 5.
          </Typography>
        </div>

        <Paper variant="outlined" className="p-6">
          <Typography variant="h6" component="h2" gutterBottom>
            Stack check
          </Typography>

          {/* The subjects of the comparison. The first is a stock MUI button;
              the second is the same button with Tailwind utilities that should
              win outright. */}
          <Stack direction="row" spacing={2} className="mb-6 items-center">
            <Button ref={plain} variant="contained">
              MUI default
            </Button>
            <Button
              ref={overridden}
              variant="contained"
              className="bg-emerald-500 px-10 hover:bg-emerald-600"
            >
              Tailwind wins
            </Button>
          </Stack>

          {checks === null ? (
            <Typography color="text.secondary">Measuring…</Typography>
          ) : (
            <Stack spacing={1}>
              {checks.map((check) => (
                <div key={check.name} className="flex items-baseline gap-3">
                  <span
                    className={
                      check.passed
                        ? 'font-mono text-sm text-emerald-600'
                        : 'font-mono text-sm text-red-600'
                    }
                  >
                    {check.passed ? 'PASS' : 'FAIL'}
                  </span>
                  <span className="flex-1">
                    <Typography component="span">{check.name}</Typography>
                    <Typography
                      component="span"
                      variant="body2"
                      color="text.secondary"
                      className="block break-all"
                    >
                      {check.detail}
                    </Typography>
                  </span>
                </div>
              ))}
            </Stack>
          )}
        </Paper>

        {checks !== null && (
          <Alert severity={failed.length === 0 ? 'success' : 'error'}>
            {failed.length === 0
              ? 'Tailwind utilities override MUI defaults, with no !important anywhere.'
              : `${failed.length} check(s) failed — see above.`}
          </Alert>
        )}

        <Typography variant="body2" color="text.secondary">
          API base URL: <code>{import.meta.env.VITE_API_BASE_URL}</code> · route:{' '}
          <code>{location.pathname}</code>
        </Typography>
      </Stack>
    </Container>
  )
}
