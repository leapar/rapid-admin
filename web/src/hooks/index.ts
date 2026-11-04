/**
 * hooks 统一导出
 */
export { usePermission } from './usePermission'
export { useAuth } from './useAuth'
export { useRequest } from './useRequest'
export type { UseRequestOptions, UseRequestResult } from './useRequest'
export { useTable } from './useTable'
export type { UseTableOptions, UseTableResult } from './useTable'
export { useTableSelection } from './useTableSelection'
export type {
  UseTableSelectionOptions,
  UseTableSelectionResult,
  SelectionKey,
} from './useTableSelection'
export { useDict } from './useDict'
export type { UseDictResult } from './useDict'
export { useConfig, useConfigValue } from './useConfig'
export { useOnline } from './useOnline'
export type { UseOnlineOptions } from './useOnline'
export { useEventStream, subscribe, refreshUnread, EVENT } from './useEventStream'
export type { EventName, EventPayload, EventHandlerFn, StreamStatus } from './useEventStream'
export { useTableEdit, diffPatch } from './useTableEdit'
export type { UseTableEditOptions, UseTableEditResult, EditKey } from './useTableEdit'
export { useDragSort, reorder } from './useDragSort'
export type {
  UseDragSortOptions,
  UseDragSortResult,
  DragRowProps,
  DragKey,
} from './useDragSort'
export { useSavedViews, sanitizeViews } from './useSavedViews'
export type { SavedView, UseSavedViewsResult } from './useSavedViews'
