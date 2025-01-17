import {
	AlexaInterface,
	brightnessController,
	Category,
	colorController,
	colorTemperatureController,
	Device,
	playbackController,
	powerController,
} from './types';
import { assertUnreachable } from './utils';

// for use between only home-automation-hub and Alexa connector
enum CapabilityCode {
	PowerController = 'PowerController',
	BrightnessController = 'BrightnessController',
	ColorController = 'ColorController',
	PlaybackController = 'PlaybackController',
	ColorTemperatureController = 'ColorTemperatureController',
}

interface DiscoveryFileDevice {
	id: string;
	friendly_name: string;
	description: string;
	display_category: Category;
	capability_codes: CapabilityCode[];
}

export interface DiscoveryFile {
	queue: string;
	devices: DiscoveryFileDevice[];
}

export function toAlexaStruct(file: DiscoveryFile): Device[] {
	return file.devices.map(
		(device): Device => {
			const caps: AlexaInterface[] = device.capability_codes.map(
				(code): AlexaInterface => {
					switch (code) {
						case CapabilityCode.PowerController:
							return powerController();
						case CapabilityCode.BrightnessController:
							return brightnessController();
						case CapabilityCode.ColorController:
							return colorController();
						case CapabilityCode.PlaybackController:
							return playbackController();
						case CapabilityCode.ColorTemperatureController:
							return colorTemperatureController();
						default:
							return assertUnreachable(code);
					}
				},
			);

			return {
				endpointId: device.id,
				manufacturerName: 'function61.com',
				version: '1.0',
				friendlyName: device.friendly_name,
				description: device.description,
				displayCategories: [device.display_category],
				capabilities: caps,
				cookie: {
					queue: file.queue,
				},
			};
		},
	);
}
