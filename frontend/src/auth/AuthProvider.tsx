import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import { signIn, signOut, getCurrentUser, fetchAuthSession } from 'aws-amplify/auth'
import './amplify'

interface AuthContextType {
  isAuthenticated: boolean
  isLoading: boolean
  login: (email: string, password: string) => Promise<void>
  logout: () => Promise<void>
  getToken: () => Promise<string | null>
}

const AuthContext = createContext<AuthContextType>(null!)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [isAuthenticated, setIsAuthenticated] = useState(false)
  const [isLoading, setIsLoading] = useState(true)

  useEffect(() => {
    getCurrentUser()
      .then(() => setIsAuthenticated(true))
      .catch(() => setIsAuthenticated(false))
      .finally(() => setIsLoading(false))
  }, [])

  const login = async (email: string, password: string) => {
    await signIn({ username: email, password })
    setIsAuthenticated(true)
  }

  const logout = async () => {
    await signOut()
    setIsAuthenticated(false)
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
    <AuthContext.Provider value={{ isAuthenticated, isLoading, login, logout, getToken }}>
      {children}
    </AuthContext.Provider>
  )
}

export function useAuth() {
  return useContext(AuthContext)
}
