import { Component, ReactNode } from 'react'

export default class ErrorBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false }

  static getDerivedStateFromError() {
    return { failed: true }
  }

  render() {
    if (this.state.failed) {
      return (
        <main role="alert">
          <h1>Couldn’t display this page</h1>
          <p>Reload DZ Manager to try again.</p>
          <button onClick={() => window.location.reload()}>Reload</button>
        </main>
      )
    }
    return this.props.children
  }
}
