import React from 'react'
import { Route } from 'react-router-dom'
import { Authenticated } from 'react-admin'
import Personal from './personal/Personal'
import DeviceApproval from './sonus/DeviceApproval'

const routes = [
  <Route exact path="/personal" render={() => <Personal />} key={'personal'} />,
  // Sonus: the browser sign-in's approval page, after signing in if need be (sonus/DeviceApproval)
  <Route
    exact
    path="/device"
    render={({ location }) => (
      <Authenticated location={location}>
        <DeviceApproval key={location.search} location={location} />
      </Authenticated>
    )}
    key={'device'}
  />,
]

export default routes
