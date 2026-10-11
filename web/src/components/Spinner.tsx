export function Spinner({ size = '1.25em' }: { size?: string }) {
  return <span className="spinner" style={{ width: size, height: size }} />
}

export function PageSpinner() {
  return (
    <div style={{ display: 'flex', justifyContent: 'center', padding: '4rem' }}>
      <Spinner size="2rem" />
    </div>
  )
}
