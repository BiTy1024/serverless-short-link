// Build-time branding; deploy.sh sets these VITE_ variables from the Terraform outputs.
export const brandName: string = import.meta.env.VITE_BRAND_NAME || 'Short Links'
export const brandLogoUrl: string = import.meta.env.VITE_BRAND_LOGO_URL || ''
