/** A real CRM screen in a light browser frame. Images are lazy and sized, so nothing jumps. */
export function ScreenFrame({ src, alt, label, eager = false, className = '' }: { src: string; alt: string; label?: string; eager?: boolean; className?: string }) {
  return (
    <figure className={`cw-frame ${className}`}>
      <div className="cw-frame-bar" aria-hidden>
        <i />
        <i />
        <i />
        <span>{label ?? 'yourbusiness · Cogniz CRM'}</span>
      </div>
      <img src={src} alt={alt} width={1440} height={900} loading={eager ? 'eager' : 'lazy'} decoding="async" />
      <figcaption className="cw-sample">Sample data</figcaption>
    </figure>
  );
}
