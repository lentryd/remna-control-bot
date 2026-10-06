// CtaButton — the main call to action (renew, connect a tariff): the brand
// violet as a gradient with a soft glow, so it stands out without shouting.
import { Button, type ButtonProps } from '@mantine/core'
import type { ReactNode } from 'react'

import classes from './cta-button.module.css'

export function CtaButton({ children, onClick, ...props }: ButtonProps & { children: ReactNode; onClick?: () => void }) {
    return (
        <Button
            className={classes.cta}
            gradient={{ from: 'brand.5', to: 'brand.8', deg: 135 }}
            onClick={onClick}
            radius="xl"
            variant="gradient"
            {...props}
        >
            {children}
        </Button>
    )
}
