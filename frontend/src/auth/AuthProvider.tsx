import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { signIn, signOut, getCurrentUser, fetchAuthSession } from 'aws-amplify/auth'
import './amplify'

interface AuthContextType {
  isAuthenticated: boolean
  isLoading: boolean
  isAdmin: boolean
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
  getToken: () => Promise<string | null>
}

const AuthContext = createContext<AuthContextType>(null!)

function getGroupsFromToken(token: string): string[] {
  try {
    const payload = JSON.parse(atob(token.split('.')[1]))
    return payload['cognito:groups'] || []
  } catch {
    return []
  }
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [isAdmin, setIsAdmin] = useState(false)
  const [isLoading, setIsLoading] = useState(true)

  const loadSession = async () => {
    try {
      await getCurrentUser()
      const session = await fetchAuthSession()
      const token = session.tokens?.accessToken?.toString() ?? ''
      const groups = getGroupsFromToken(token)
      setIsAuthenticated(true)
      setIsAdmin(groups.includes('admin'))
    } catch {
      setIsAuthenticated(false)
      setIsAdmin(false)
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => { loadSession() }, [])

  const login = async (email: string, password: string) => {
    await signIn({ username: email, password })
    await loadSession()
  }

  const logout = async () => {
    await signOut()
    setIsAuthenticated(false)
    setIsAdmin(false)
  }

  const getToken = async (): Promise<string | null> => {
    try {
      const session = await fetchAuthSession()
      return session.tokens?.accessToken?.toString() ?? null
    } catch {
      return null
    }
  }

  return (
    <AuthContext.Provider value={{ isAuthenticated, isLoading, isAdmin, login, logout, getToken }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
