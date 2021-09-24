import React, { ReactElement } from 'react';
import { Link } from 'react-router-dom';
import { Button } from '@patternfly/react-core';
import pluralize from 'pluralize';

import { AccessControlEntityType } from 'constants/entityTypes';
import { Role } from 'services/RolesService';

import { getEntityPath } from './accessControlPaths';

export type AccessControlEntityLinkProps = {
    entityType: AccessControlEntityType;
    entityId: string;
    entityName: string;
};

export function AccessControlEntityLink({
    entityType,
    entityId,
    entityName,
}: AccessControlEntityLinkProps): ReactElement {
    return (
        <Button
            variant="link"
            isInline
            component={(props) => <Link {...props} to={getEntityPath(entityType, entityId)} />}
        >
            {entityName}
        </Button>
    );
}

export type RolesLinkProps = {
    roles: Role[];
    entityType: AccessControlEntityType;
    entityId: string;
};

export function RolesLink({ roles, entityType, entityId }: RolesLinkProps): ReactElement {
    if (roles.length === 0) {
        return <span>No roles</span>;
    }

    if (roles.length === 1) {
        const { name } = roles[0];
        // The name is the id for a role.
        return <AccessControlEntityLink entityType="ROLE" entityId={name} entityName={name} />;
    }

    const count = roles.length;
    const url = getEntityPath('ROLE', '', { s: { [entityType]: entityId } });
    const text = `${count} ${pluralize('role', count)}`;
    return (
        <Button variant="link" isInline component={(props) => <Link {...props} to={url} />}>
            {text}
        </Button>
    );
}
