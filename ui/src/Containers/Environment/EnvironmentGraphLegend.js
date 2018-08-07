import React from 'react';
import * as Icon from 'react-feather';

const EnvironmentGraphLegend = () => {
    const items = [
        {
            label: 'Deployment',
            icon: <Icon.Circle className="h-2 w-2" color="#3F4884" fill="#3F4884" />
        },
        {
            label: 'Namespace',
            icon: <Icon.Square className="h-2 w-2" color="#3F4884" strokeWidth="3" />
        },
        {
            label: 'Ingress/Egress',
            icon: <Icon.ArrowRight className="h-2 w-2" color="#b3b3b3" strokeWidth="3" />
        },
        {
            label: 'Internet Egress',
            icon: <Icon.Circle className="h-2 w-2" color="#fdc9ef" strokeWidth="4" />
        }
    ];
    return (
        <div className="env-graph-legend absolute pin-t pin-l mt-2 ml-2 bg-white text-primary-500 border-primary-500 border rounded-sm">
            {items.map((item, index) => (
                <div className="p-2" key={index}>
                    {item.icon}
                    <span className="pl-2">{item.label}</span>
                </div>
            ))}
        </div>
    );
};

export default EnvironmentGraphLegend;
