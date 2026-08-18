import { configureStore } from '@reduxjs/toolkit'

/**
 * The Redux store.
 *
 * There are no slices yet — `authSlice` arrives in Phase 5. Until then the store
 * is built from a root reducer function rather than an empty `{}` reducer map,
 * because `combineReducers` with no keys logs "Store does not have a valid
 * reducer" on every dispatch. Phase 5 replaces this one argument with the map:
 *
 *   reducer: { auth: authReducer }
 *
 * Everything below it — the types and the hooks in ./hooks.ts — survives that
 * change untouched, which is the point of wiring the Provider now.
 */
const noSlicesYet = (state: Record<string, never> = {}) => state

export const store = configureStore({
  reducer: noSlicesYet,
})

export type AppStore = typeof store
export type RootState = ReturnType<AppStore['getState']>
export type AppDispatch = AppStore['dispatch']
