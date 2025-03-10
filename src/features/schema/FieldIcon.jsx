import { FontAwesomeIcon } from '@fortawesome/react-fontawesome';
import { findIconDefinition } from '@fortawesome/fontawesome-svg-core';

import Icon from '@mui/material/Icon';

import * as styles from './styles.css';

const { library } = await import('@fortawesome/fontawesome-svg-core');
const { fab } = await import('@fortawesome/free-brands-svg-icons');
const { fas } = await import('@fortawesome/free-solid-svg-icons');
library.add(fas, fab);

export default function FieldIcon(props) {
    const iconName = props.icon;

    if (!iconName) {
        return null;
    }

    const faIconName = iconName.replace(/[A-Z]/g, m => "-" + m.toLowerCase());

    let icoDef = findIconDefinition({ prefix: 'fas', iconName: faIconName });
    if (icoDef) {
        return <FontAwesomeIcon icon={icoDef} />;
    }

    icoDef = findIconDefinition({ prefix: 'fab', iconName: faIconName });
    if (icoDef) {
        return <FontAwesomeIcon icon={icoDef} />;
    }

    return (
        <Icon className={styles.materialIcons}>{iconName}</Icon>
    )
}