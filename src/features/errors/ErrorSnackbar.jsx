import { createRef } from 'react';
import { SnackbarProvider } from 'notistack';
import * as styles from './styles.css';

export default function ErrorSnackbar(props) {
    const notistackRef = createRef();

    return (
        <SnackbarProvider
            hideIconVariant
            preventDuplicate
            maxSnack={10}
            ref={notistackRef}
            classes={{
                variantError: styles.error,
            }}
            anchorOrigin={{
                vertical: 'bottom',
                horizontal: 'center',
            }}
        >
            {props.children}
        </SnackbarProvider>
    )
};