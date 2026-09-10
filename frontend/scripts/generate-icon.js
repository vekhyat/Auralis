import sharp from 'sharp';
import { readFileSync, mkdirSync, writeFileSync } from 'fs';
import { dirname, join } from 'path';
import { fileURLToPath } from 'url';
const __dirname = dirname(fileURLToPath(import.meta.url));
const rootDir = join(__dirname, '..', '..');
const svgPath = join(rootDir, 'frontend', 'public', 'icon.svg');
const outputPath = join(rootDir, 'build', 'appicon.png');
const icoOutputPath = join(rootDir, 'build', 'windows', 'icon.ico');
async function generateIcon() {
    try {
        mkdirSync(join(rootDir, 'build'), { recursive: true });
        const svgBuffer = readFileSync(svgPath);
        await sharp(svgBuffer)
            .resize(1024, 1024)
            .png()
            .toFile(outputPath);
        console.log('Icon generated:', outputPath);
        // Windows executable icon: ICO container embedding PNG frames.
        const sizes = [256, 48, 32, 16];
        const frames = [];
        for (const size of sizes) {
            const png = await sharp(svgBuffer).resize(size, size).png().toBuffer();
            frames.push({ size, png });
        }
        const headerSize = 6 + frames.length * 16;
        let offset = headerSize;
        const chunks = [Buffer.alloc(headerSize)];
        chunks[0].writeUInt16LE(0, 0); // reserved
        chunks[0].writeUInt16LE(1, 2); // type: icon
        chunks[0].writeUInt16LE(frames.length, 4);
        frames.forEach((frame, index) => {
            const entry = index * 16 + 6;
            chunks[0].writeUInt8(frame.size >= 256 ? 0 : frame.size, entry);
            chunks[0].writeUInt8(frame.size >= 256 ? 0 : frame.size, entry + 1);
            chunks[0].writeUInt8(0, entry + 2); // palette
            chunks[0].writeUInt8(0, entry + 3); // reserved
            chunks[0].writeUInt16LE(1, entry + 4); // color planes
            chunks[0].writeUInt16LE(32, entry + 6); // bits per pixel
            chunks[0].writeUInt32LE(frame.png.length, entry + 8);
            chunks[0].writeUInt32LE(offset, entry + 12);
            offset += frame.png.length;
        });
        for (const frame of frames) {
            chunks.push(frame.png);
        }
        mkdirSync(join(rootDir, 'build', 'windows'), { recursive: true });
        writeFileSync(icoOutputPath, Buffer.concat(chunks));
        console.log('Icon generated:', icoOutputPath);
    }
    catch (error) {
        console.error('Failed to generate icon:', error.message);
        process.exit(1);
    }
}
generateIcon();
