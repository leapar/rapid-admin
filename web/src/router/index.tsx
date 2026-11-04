/**
 * Router 入口
 */
import React from 'react'
import { createBrowserRouter, RouterProvider } from 'react-router-dom'
import { routes } from './routes'

export const router = createBrowserRouter(routes)

export const Router: React.FC = () => <RouterProvider router={router} />

export default Router
