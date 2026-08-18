import { useDispatch, useSelector } from 'react-redux'

import type { AppDispatch, RootState } from './store.ts'

/**
 * Typed versions of the react-redux hooks. Components use these instead of the
 * plain ones, so state and dispatch — including thunks — carry their types
 * without an annotation at every call site.
 */
export const useAppDispatch = useDispatch.withTypes<AppDispatch>()
export const useAppSelector = useSelector.withTypes<RootState>()
