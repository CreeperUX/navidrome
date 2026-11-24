import { jwtDecode } from 'jwt-decode'
import { baseUrl } from './utils'
import config from './config'

// config sent from server may contain authentication info, for example when the user is authenticated
// by a reverse proxy request header
if (config.auth) {
  try {
    storeAuthenticationInfo(config.auth)
  } catch (e) {
    // eslint-disable-next-line no-console
    console.log(e)
  }
}

function storeAuthenticationInfo(authInfo) {
  authInfo.token && localStorage.setItem('token', authInfo.token)
  localStorage.setItem('userId', authInfo.id)
  localStorage.setItem('name', authInfo.name)
  localStorage.setItem('username', authInfo.username)
  authInfo.avatar && localStorage.setItem('avatar', authInfo.avatar)
  localStorage.setItem('role', authInfo.isAdmin ? 'admin' : 'regular')
  // Sonus: with two-step verification there is no password-derived Subsonic token; the session signs those calls
  // (subsonic/index.js)
  if (authInfo.subsonicToken && authInfo.subsonicSalt) {
    localStorage.setItem('subsonic-salt', authInfo.subsonicSalt)
    localStorage.setItem('subsonic-token', authInfo.subsonicToken)
  } else {
    localStorage.removeItem('subsonic-salt')
    localStorage.removeItem('subsonic-token')
  }
  localStorage.setItem('is-authenticated', 'true')
}

const authProvider = {
  login: ({ username, password, otp, twoFactorResult }) => {
    // Sonus: two-step verification just set up answers like a sign-in (sonus/TwoFactorSetup)
    if (twoFactorResult) {
      jwtDecode(twoFactorResult.token) // Validate token
      storeAuthenticationInfo(twoFactorResult)
      config.firstTime = false
      return Promise.resolve(twoFactorResult)
    }
    let url = baseUrl('/auth/login')
    if (config.firstTime) {
      url = baseUrl('/auth/createAdmin')
    }
    const request = new Request(url, {
      method: 'POST',
      body: JSON.stringify({ username, password, ...(otp ? { otp } : {}) }),
      headers: new Headers({ 'Content-Type': 'application/json' }),
    })
    return fetch(request)
      .then(async (response) => {
        if (response.status < 200 || response.status >= 300) {
          // Sonus: the password was right, and two-step verification wants a code, or to be set up first
          const body = await response.json().catch(() => ({}))
          if (body.twoFactor) {
            const error = new Error(body.error || response.statusText)
            error.twoFactor = body.twoFactor
            error.setupToken = body.setupToken
            // the Sonus server: the set-up takes the enrolment code an admin handed over
            error.enrolmentCodeRequired = body.enrolmentCodeRequired === true
            throw error
          }
          throw new Error(response.statusText)
        }
        return response.json()
      })
      .then((response) => {
        jwtDecode(response.token) // Validate token
        storeAuthenticationInfo(response)
        // Avoid "going to create admin" dialog after logout/login without a refresh
        config.firstTime = false
        return response
      })
      .catch((error) => {
        if (
          error.message === 'Failed to fetch' ||
          error.stack === 'TypeError: Failed to fetch'
        ) {
          throw new Error('errors.network_error')
        }
        if (error.twoFactor) {
          throw error // the sign-in page goes on to the second step
        }

        throw new Error(error)
      })
  },

  logout: () => {
    removeItems()
    // Only proxy-authenticated sessions go to the IdP; others (e.g. direct LAN access) get the login form
    if (config.extAuthLogoutURL && config.auth) {
      window.location.href = config.extAuthLogoutURL
      return Promise.resolve(false)
    }
    return Promise.resolve()
  },

  checkAuth: () =>
    localStorage.getItem('is-authenticated')
      ? Promise.resolve()
      : Promise.reject(),

  checkError: ({ status }) => {
    if (status === 401) {
      removeItems()
      return Promise.reject()
    }
    return Promise.resolve()
  },

  getPermissions: () => {
    const role = localStorage.getItem('role')
    return role ? Promise.resolve(role) : Promise.reject()
  },

  getIdentity: () => {
    return Promise.resolve({
      id: localStorage.getItem('username'),
      fullName: localStorage.getItem('name'),
      avatar: localStorage.getItem('avatar'),
    })
  },
}

const removeItems = () => {
  localStorage.removeItem('token')
  localStorage.removeItem('userId')
  localStorage.removeItem('name')
  localStorage.removeItem('username')
  localStorage.removeItem('avatar')
  localStorage.removeItem('role')
  localStorage.removeItem('subsonic-salt')
  localStorage.removeItem('subsonic-token')
  localStorage.removeItem('is-authenticated')
}

export default authProvider
